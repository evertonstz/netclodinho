/**
 * Connect client for bidirectional communication with control plane.
 *
 * The agent connects TO the control plane (not the other way around),
 * establishing a bidirectional stream for all communication.
 */

import { createClient } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";
import { create } from "@bufbuild/protobuf";
import { timestampNow } from "@bufbuild/protobuf/wkt";

import {
  AgentService,
  AgentMessageSchema,
  AgentStreamResponseSchema,
  AgentTitleResponseSchema,
  AgentGitStatusResponseSchema,
  AgentGitDiffResponseSchema,
  AgentTerminalOutputSchema,
  AgentRegisterSchema,
  AgentSnapshotResultSchema,
  type AgentMessage,
  type ControlPlaneMessage,
  type AgentStreamResponse,
} from "../gen/netclode/v1/agent_pb.js";
import {
  AgentEventKind,
  AgentEventSchema,
  ToolEventPayloadSchema,
  ThinkingPayloadSchema,
  RepoClonePayloadSchema,
  RepoCloneStage,
} from "../gen/netclode/v1/events_pb.js";
import {
  GitFileStatus,
  GitFileChangeSchema,
  CopilotBackend as ProtoCopilotBackend,
  type SessionConfig,
} from "../gen/netclode/v1/common_pb.js";

// Import modular services
import { handleTerminalInput, resizeTerminal, setTerminalOutputCallback } from "./services/terminal.js";
import { generateTitle } from "./services/title.js";
import { getGitStatus, getGitDiff, type GitFileChange } from "./git.js";
import { createSnapshot, restoreSnapshot } from "./services/snapshot.js";

// Import SDK abstraction layer
import {
  createSDKAdapter,
  parseSdkType,
  getAdapter,
  shutdownAllAdapters,
  type SDKAdapter,
  type PromptEvent,
  type SdkType,
  type CopilotBackend,
} from "./sdk/index.js";
import { SdkType as ProtoSdkType } from "../gen/netclode/v1/common_pb.js";

const WORKSPACE_DIR = "/agent/workspace";

// Track if a prompt is currently running (to prevent concurrent prompts)
let isPromptRunning = false;

// Current SDK adapter for the session
let currentAdapter: SDKAdapter | null = null;

/**
 * Convert proto SdkType enum to internal SdkType string
 */
function parseSdkTypeFromProto(protoSdkType: ProtoSdkType | undefined): SdkType {
  switch (protoSdkType) {
    case ProtoSdkType.OPENCODE:
      return "opencode";
    case ProtoSdkType.COPILOT:
      return "copilot";
    case ProtoSdkType.CLAUDE:
    case ProtoSdkType.UNSPECIFIED:
    default:
      return "claude";
  }
}

/**
 * Convert proto CopilotBackend enum to internal CopilotBackend string
 */
function parseCopilotBackendFromProto(protoBackend: ProtoCopilotBackend | undefined): CopilotBackend | undefined {
  switch (protoBackend) {
    case ProtoCopilotBackend.GITHUB:
      return "github";
    case ProtoCopilotBackend.ANTHROPIC:
      return "anthropic";
    case ProtoCopilotBackend.UNSPECIFIED:
    default:
      return undefined; // Let adapter auto-detect
  }
}

/**
 * Convert local repo clone stage to protobuf enum
 */
function convertRepoCloneStage(stage: "cloning" | "done" | "error"): RepoCloneStage {
  switch (stage) {
    case "cloning": return RepoCloneStage.CLONING;
    case "done": return RepoCloneStage.DONE;
    case "error": return RepoCloneStage.ERROR;
    default: return RepoCloneStage.UNSPECIFIED;
  }
}

/**
 * Convert local git status to protobuf enum
 */
function convertGitStatus(status: GitFileChange["status"]): GitFileStatus {
  switch (status) {
    case "modified": return GitFileStatus.MODIFIED;
    case "added": return GitFileStatus.ADDED;
    case "deleted": return GitFileStatus.DELETED;
    case "renamed": return GitFileStatus.RENAMED;
    case "untracked": return GitFileStatus.UNTRACKED;
    case "copied": return GitFileStatus.COPIED;
    case "ignored": return GitFileStatus.IGNORED;
    case "unmerged": return GitFileStatus.UNMERGED;
    default: return GitFileStatus.UNSPECIFIED;
  }
}

/**
 * Convert prompt event to AgentMessage
 */
function promptEventToAgentMessage(event: PromptEvent): AgentMessage {
  const timestamp = timestampNow();
  let response: AgentStreamResponse["response"];

  switch (event.type) {
    case "system":
      response = {
        case: "systemMessage",
        value: { $typeName: "netclode.v1.AgentSystemMessage", message: event.message },
      };
      break;

    case "textDelta":
      response = {
        case: "textDelta",
        value: { $typeName: "netclode.v1.AgentTextDelta", content: event.content, partial: event.partial, messageId: "" },
      };
      break;

    case "toolStart":
      response = {
        case: "event",
        value: create(AgentEventSchema, {
          kind: AgentEventKind.TOOL_START,
          timestamp,
          payload: {
            case: "tool",
            value: create(ToolEventPayloadSchema, {
              tool: event.tool,
              toolUseId: event.toolUseId,
              ...(event.parentToolUseId && { parentToolUseId: event.parentToolUseId }),
              ...(event.input && { input: event.input }),
            }),
          },
        }),
      };
      break;

    case "toolInput":
      response = {
        case: "event",
        value: create(AgentEventSchema, {
          kind: AgentEventKind.TOOL_INPUT,
          timestamp,
          payload: {
            case: "tool",
            value: create(ToolEventPayloadSchema, {
              tool: "",
              toolUseId: event.toolUseId,
              inputDelta: event.inputDelta,
              ...(event.parentToolUseId && { parentToolUseId: event.parentToolUseId }),
            }),
          },
        }),
      };
      break;

    case "toolInputComplete":
      response = {
        case: "event",
        value: create(AgentEventSchema, {
          kind: AgentEventKind.TOOL_INPUT_COMPLETE,
          timestamp,
          payload: {
            case: "tool",
            value: create(ToolEventPayloadSchema, {
              tool: "",
              toolUseId: event.toolUseId,
              input: event.input,
              ...(event.parentToolUseId && { parentToolUseId: event.parentToolUseId }),
            }),
          },
        }),
      };
      break;

    case "toolEnd":
      response = {
        case: "event",
        value: create(AgentEventSchema, {
          kind: AgentEventKind.TOOL_END,
          timestamp,
          payload: {
            case: "tool",
            value: create(ToolEventPayloadSchema, {
              tool: event.tool,
              toolUseId: event.toolUseId,
              result: event.result,
              error: event.error,
              ...(event.parentToolUseId && { parentToolUseId: event.parentToolUseId }),
              ...(event.durationMs !== undefined && { durationMs: BigInt(event.durationMs) }),
            }),
          },
        }),
      };
      break;

    case "thinking":
      response = {
        case: "event",
        value: create(AgentEventSchema, {
          kind: AgentEventKind.THINKING,
          timestamp,
          payload: {
            case: "thinking",
            value: create(ThinkingPayloadSchema, {
              thinkingId: event.thinkingId,
              content: event.content,
              partial: event.partial,
            }),
          },
        }),
      };
      break;

    case "repoClone":
      response = {
        case: "event",
        value: create(AgentEventSchema, {
          kind: AgentEventKind.REPO_CLONE,
          timestamp,
          payload: {
            case: "repoClone",
            value: create(RepoClonePayloadSchema, {
              stage: convertRepoCloneStage(event.stage),
              repo: event.repo,
              message: event.message,
            }),
          },
        }),
      };
      break;

    case "result":
      response = {
        case: "result",
        value: {
          $typeName: "netclode.v1.AgentResult",
          inputTokens: event.inputTokens,
          outputTokens: event.outputTokens,
          totalTurns: event.totalTurns,
        },
      };
      break;

    case "error":
      response = {
        case: "error",
        value: { $typeName: "netclode.v1.AgentError", message: event.message, retryable: event.retryable },
      };
      break;

    default:
      response = { case: undefined, value: undefined };
  }

  return create(AgentMessageSchema, {
    message: {
      case: "promptResponse",
      value: create(AgentStreamResponseSchema, { response }),
    },
  });
}

/**
 * Agent connection state
 */
interface AgentConnection {
  sessionConfig: SessionConfig | null;
  send: (msg: AgentMessage) => void;
}

let connection: AgentConnection | null = null;

/**
 * Connect to the control plane and handle bidirectional communication.
 */
export async function connectToControlPlane(
  controlPlaneUrl: string,
  sessionId: string
): Promise<void> {
  console.log(`[agent] Connecting to control plane at ${controlPlaneUrl}`);

  const transport = createGrpcTransport({
    baseUrl: controlPlaneUrl,
  });

  const client = createClient(AgentService, transport);

  // Create async generator for outgoing messages
  const outgoingMessages: AgentMessage[] = [];
  let resolveNext: ((value: AgentMessage) => void) | null = null;

  const sendMessage = (msg: AgentMessage) => {
    if (resolveNext) {
      resolveNext(msg);
      resolveNext = null;
    } else {
      outgoingMessages.push(msg);
    }
  };

  async function* messageGenerator(): AsyncIterable<AgentMessage> {
    // First, send registration
    yield create(AgentMessageSchema, {
      message: {
        case: "register",
        value: create(AgentRegisterSchema, {
          sessionId,
          version: "1.0.0",
        }),
      },
    });

    // Then yield any queued or future messages
    while (true) {
      if (outgoingMessages.length > 0) {
        yield outgoingMessages.shift()!;
      } else {
        yield await new Promise<AgentMessage>((resolve) => {
          resolveNext = resolve;
        });
      }
    }
  }

  // Set up terminal output callback to forward to CP
  setTerminalOutputCallback((data: string) => {
    if (connection) {
      connection.send(
        create(AgentMessageSchema, {
          message: {
            case: "terminalOutput",
            value: create(AgentTerminalOutputSchema, { data }),
          },
        })
      );
    }
  });

  try {
    // Start bidirectional stream
    const stream = client.connect(messageGenerator());

    connection = {
      sessionConfig: null,
      send: sendMessage,
    };

    // Handle incoming messages from control plane
    for await (const msg of stream) {
      await handleControlPlaneMessage(sessionId, msg, sendMessage);
    }
  } finally {
    // Clear terminal output callback
    setTerminalOutputCallback(null);
    connection = null;

    // Shutdown SDK adapters
    if (currentAdapter) {
      try {
        await currentAdapter.shutdown();
      } catch (err) {
        console.error("[agent] Error shutting down SDK adapter:", err);
      }
      currentAdapter = null;
    }

    console.log("[agent] Disconnected from control plane");
  }
}

/**
 * Handle a message from the control plane.
 */
async function handleControlPlaneMessage(
  sessionId: string,
  msg: ControlPlaneMessage,
  send: (msg: AgentMessage) => void
): Promise<void> {
  switch (msg.message.case) {
    case "registered":
      if (msg.message.value.success) {
        console.log("[agent] Registered with control plane");
        if (connection && msg.message.value.config) {
          connection.sessionConfig = msg.message.value.config;

          // Initialize SDK adapter based on session config
          const config = msg.message.value.config;
          const sdkType = parseSdkTypeFromProto(config.sdkType);
          const copilotBackend = parseCopilotBackendFromProto(config.copilotBackend);
          console.log(`[agent] Initializing SDK adapter: ${sdkType}, model: ${config.model || "(default)"}, copilotBackend: ${copilotBackend || "(auto)"}`);

          try {
            currentAdapter = await createSDKAdapter({
              sdkType,
              workspaceDir: WORKSPACE_DIR,
              anthropicApiKey: process.env.ANTHROPIC_API_KEY || "",
              githubToken: config.githubToken,
              model: config.model,
              copilotBackend,
            });
          } catch (err) {
            console.error("[agent] Failed to initialize SDK adapter:", err);
            throw new Error(`SDK initialization failed: ${err instanceof Error ? err.message : String(err)}`);
          }
        }
      } else {
        console.error("[agent] Registration failed:", msg.message.value.error);
        throw new Error(`Registration failed: ${msg.message.value.error}`);
      }
      break;

    case "executePrompt":
      if (isPromptRunning) {
        console.warn("[agent] Prompt already running, ignoring new prompt");
        break;
      }
      // Don't await - run concurrently so interrupt/terminal messages can be processed
      isPromptRunning = true;
      handleExecutePrompt(sessionId, msg.message.value.text, send)
        .catch((err) => {
          console.error("[agent] Prompt execution error (async):", err);
        })
        .finally(() => {
          isPromptRunning = false;
        });
      break;

    case "interrupt":
      console.log("[agent] Interrupt requested");
      if (currentAdapter) {
        currentAdapter.setInterruptSignal();
      }
      break;

    case "generateTitle":
      await handleGenerateTitle(msg.message.value.requestId, msg.message.value.prompt, send);
      break;

    case "getGitStatus":
      await handleGetGitStatus(msg.message.value.requestId, send);
      break;

    case "getGitDiff":
      await handleGetGitDiff(msg.message.value.requestId, msg.message.value.file, send);
      break;

    case "terminalInput":
      handleTerminalInputMessage(msg.message.value);
      break;

    case "createSnapshot":
      await handleCreateSnapshot(
        msg.message.value.requestId,
        msg.message.value.snapshotId,
        msg.message.value.name,
        send
      );
      break;

    case "restoreSnapshot":
      await handleRestoreSnapshot(msg.message.value.requestId, msg.message.value.snapshotId, send);
      break;

    default:
      console.warn("[agent] Unknown control plane message:", msg.message.case);
  }
}

/**
 * Handle execute prompt request
 */
async function handleExecutePrompt(
  sessionId: string,
  text: string,
  send: (msg: AgentMessage) => void
): Promise<void> {
  const config = connection?.sessionConfig;

  if (!currentAdapter) {
    console.error("[agent] No SDK adapter initialized");
    send(
      create(AgentMessageSchema, {
        message: {
          case: "promptResponse",
          value: create(AgentStreamResponseSchema, {
            response: {
              case: "error",
              value: {
                $typeName: "netclode.v1.AgentError",
                message: "SDK adapter not initialized",
                retryable: false,
              },
            },
          }),
        },
      })
    );
    return;
  }

  try {
    for await (const event of currentAdapter.executePrompt(
      sessionId,
      text,
      config ? { repo: config.repo, githubToken: config.githubToken } : undefined
    )) {
      send(promptEventToAgentMessage(event));
    }
  } catch (error) {
    console.error("[agent] Prompt execution error:", error);
    send(
      create(AgentMessageSchema, {
        message: {
          case: "promptResponse",
          value: create(AgentStreamResponseSchema, {
            response: {
              case: "error",
              value: {
                $typeName: "netclode.v1.AgentError",
                message: error instanceof Error ? error.message : String(error),
                retryable: false,
              },
            },
          }),
        },
      })
    );
  }
}

/**
 * Handle generate title request
 */
async function handleGenerateTitle(
  requestId: string,
  prompt: string,
  send: (msg: AgentMessage) => void
): Promise<void> {
  try {
    const title = await generateTitle(prompt);
    send(
      create(AgentMessageSchema, {
        message: {
          case: "titleResponse",
          value: create(AgentTitleResponseSchema, { requestId, title }),
        },
      })
    );
  } catch (error) {
    console.error("[agent] Title generation error:", error);
    send(
      create(AgentMessageSchema, {
        message: {
          case: "titleResponse",
          value: create(AgentTitleResponseSchema, { requestId, title: "" }),
        },
      })
    );
  }
}

/**
 * Handle get git status request
 */
async function handleGetGitStatus(
  requestId: string,
  send: (msg: AgentMessage) => void
): Promise<void> {
  try {
    const files = await getGitStatus(WORKSPACE_DIR);
    send(
      create(AgentMessageSchema, {
        message: {
          case: "gitStatusResponse",
          value: create(AgentGitStatusResponseSchema, {
            requestId,
            files: files.map((f) =>
              create(GitFileChangeSchema, {
                path: f.path,
                status: convertGitStatus(f.status),
                staged: f.staged,
              })
            ),
          }),
        },
      })
    );
  } catch (error) {
    console.error("[agent] Git status error:", error);
    send(
      create(AgentMessageSchema, {
        message: {
          case: "gitStatusResponse",
          value: create(AgentGitStatusResponseSchema, { requestId, files: [] }),
        },
      })
    );
  }
}

/**
 * Handle get git diff request
 */
async function handleGetGitDiff(
  requestId: string,
  file: string | undefined,
  send: (msg: AgentMessage) => void
): Promise<void> {
  try {
    const diff = await getGitDiff(WORKSPACE_DIR, file);
    send(
      create(AgentMessageSchema, {
        message: {
          case: "gitDiffResponse",
          value: create(AgentGitDiffResponseSchema, { requestId, diff }),
        },
      })
    );
  } catch (error) {
    console.error("[agent] Git diff error:", error);
    send(
      create(AgentMessageSchema, {
        message: {
          case: "gitDiffResponse",
          value: create(AgentGitDiffResponseSchema, { requestId, diff: "" }),
        },
      })
    );
  }
}

/**
 * Handle terminal input from control plane
 */
function handleTerminalInputMessage(input: {
  input: { case: "data"; value: string } | { case: "resize"; value: { cols: number; rows: number } } | { case: undefined; value?: undefined };
}): void {
  switch (input.input.case) {
    case "data":
      handleTerminalInput(input.input.value);
      break;
    case "resize":
      resizeTerminal(input.input.value.cols, input.input.value.rows);
      break;
  }
}

/**
 * Handle create snapshot command
 */
async function handleCreateSnapshot(
  requestId: string,
  snapshotId: string,
  name: string,
  send: (msg: AgentMessage) => void
): Promise<void> {
  const result = await createSnapshot(snapshotId, name);
  send(
    create(AgentMessageSchema, {
      message: {
        case: "snapshotResult",
        value: create(AgentSnapshotResultSchema, {
          requestId,
          success: result.success,
          error: result.error,
          sizeBytes: result.sizeBytes ? BigInt(result.sizeBytes) : undefined,
        }),
      },
    })
  );
}

/**
 * Handle restore snapshot command
 */
async function handleRestoreSnapshot(
  requestId: string,
  snapshotId: string,
  send: (msg: AgentMessage) => void
): Promise<void> {
  const result = await restoreSnapshot(snapshotId);
  send(
    create(AgentMessageSchema, {
      message: {
        case: "snapshotResult",
        value: create(AgentSnapshotResultSchema, {
          requestId,
          success: result.success,
          error: result.error,
        }),
      },
    })
  );
}
