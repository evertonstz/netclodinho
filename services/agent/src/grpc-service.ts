/**
 * gRPC/Connect service implementation
 * 
 * This is the main orchestrator that wires up the modular services
 * to the Connect protocol handlers.
 */

import { type ServiceImpl } from "@connectrpc/connect";
import { create } from "@bufbuild/protobuf";
import { timestampNow } from "@bufbuild/protobuf/wkt";

import { AgentService } from "../gen/netclode/v1/agent_pb.js";
import {
  AgentStreamResponseSchema,
  GenerateTitleResponseSchema,
  GetGitDiffResponseSchema,
  GetGitStatusResponseSchema,
  HealthResponseSchema,
  InterruptResponseSchema,
  TerminalOutputSchema,
  type ExecutePromptRequest,
  type GenerateTitleRequest,
  type GetGitDiffRequest,
  type TerminalInput,
} from "../gen/netclode/v1/agent_pb.js";
import {
  AgentEventKind,
  AgentEventSchema,
} from "../gen/netclode/v1/events_pb.js";
import {
  GitFileStatus,
  GitFileChangeSchema,
} from "../gen/netclode/v1/common_pb.js";

// Import modular services
import { executePrompt, type PromptEvent } from "./services/prompt.js";
import { handleTerminalStream } from "./services/terminal.js";
import { generateTitle } from "./services/title.js";
import { getGitStatus, getGitDiff, type GitFileChange } from "./git.js";

const WORKSPACE_DIR = "/agent/workspace";

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
 * Convert prompt event to protobuf response
 */
function* promptEventToResponse(event: PromptEvent) {
  const timestamp = timestampNow();

  switch (event.type) {
    case "system":
      yield create(AgentStreamResponseSchema, {
        response: {
          case: "systemMessage",
          value: { message: event.message },
        },
      });
      break;

    case "textDelta":
      yield create(AgentStreamResponseSchema, {
        response: {
          case: "textDelta",
          value: { content: event.content, partial: event.partial, messageId: "" },
        },
      });
      break;

    case "toolStart":
      yield create(AgentStreamResponseSchema, {
        response: {
          case: "event",
          value: create(AgentEventSchema, {
            kind: AgentEventKind.TOOL_START,
            tool: event.tool,
            toolUseId: event.toolUseId,
            timestamp,
            ...(event.parentToolUseId && { parentToolUseId: event.parentToolUseId }),
            ...(event.input && { input: event.input }),
          }),
        },
      });
      break;

    case "toolInput":
      yield create(AgentStreamResponseSchema, {
        response: {
          case: "event",
          value: create(AgentEventSchema, {
            kind: AgentEventKind.TOOL_INPUT,
            toolUseId: event.toolUseId,
            inputDelta: event.inputDelta,
            timestamp,
            ...(event.parentToolUseId && { parentToolUseId: event.parentToolUseId }),
          }),
        },
      });
      break;

    case "toolInputComplete":
      yield create(AgentStreamResponseSchema, {
        response: {
          case: "event",
          value: create(AgentEventSchema, {
            kind: AgentEventKind.TOOL_INPUT_COMPLETE,
            toolUseId: event.toolUseId,
            input: event.input,
            timestamp,
            ...(event.parentToolUseId && { parentToolUseId: event.parentToolUseId }),
          }),
        },
      });
      break;

    case "toolEnd":
      yield create(AgentStreamResponseSchema, {
        response: {
          case: "event",
          value: create(AgentEventSchema, {
            kind: AgentEventKind.TOOL_END,
            tool: event.tool,
            toolUseId: event.toolUseId,
            result: event.result,
            error: event.error,
            timestamp,
            ...(event.parentToolUseId && { parentToolUseId: event.parentToolUseId }),
          }),
        },
      });
      break;

    case "thinking":
      yield create(AgentStreamResponseSchema, {
        response: {
          case: "event",
          value: create(AgentEventSchema, {
            kind: AgentEventKind.THINKING,
            thinkingId: event.thinkingId,
            content: event.content,
            partial: event.partial,
            timestamp,
          }),
        },
      });
      break;

    case "repoClone":
      yield create(AgentStreamResponseSchema, {
        response: {
          case: "event",
          value: create(AgentEventSchema, {
            kind: AgentEventKind.REPO_CLONE,
            stage: event.stage,
            repo: event.repo,
            message: event.message,
            timestamp,
          }),
        },
      });
      break;

    case "result":
      yield create(AgentStreamResponseSchema, {
        response: {
          case: "result",
          value: {
            inputTokens: event.inputTokens,
            outputTokens: event.outputTokens,
            totalTurns: event.totalTurns,
          },
        },
      });
      break;

    case "error":
      yield create(AgentStreamResponseSchema, {
        response: {
          case: "error",
          value: { message: event.message, retryable: event.retryable },
        },
      });
      break;
  }
}

export const agentServiceImpl: ServiceImpl<typeof AgentService> = {
  /**
   * ExecutePrompt sends a prompt to the agent and streams back responses
   */
  async *executePrompt(req: ExecutePromptRequest) {
    const { sessionId, text, config } = req;

    for await (const event of executePrompt(sessionId, text, config ? { repo: config.repo, githubToken: config.githubToken } : undefined)) {
      yield* promptEventToResponse(event);
    }
  },

  /**
   * Interrupt stops the current agent execution
   */
  async interrupt() {
    console.log("[agent] Interrupt requested");
    return create(InterruptResponseSchema, { success: true });
  },

  /**
   * GenerateTitle generates a session title based on conversation
   */
  async generateTitle(req: GenerateTitleRequest) {
    const title = await generateTitle(req.prompt);
    return create(GenerateTitleResponseSchema, { title });
  },

  /**
   * GetGitStatus returns the git status of the workspace
   */
  async getGitStatus() {
    const files = await getGitStatus(WORKSPACE_DIR);
    return create(GetGitStatusResponseSchema, {
      files: files.map((f) =>
        create(GitFileChangeSchema, {
          path: f.path,
          status: convertGitStatus(f.status),
          staged: f.staged,
        })
      ),
    });
  },

  /**
   * GetGitDiff returns the git diff for the workspace or a specific file
   */
  async getGitDiff(req: GetGitDiffRequest) {
    const diff = await getGitDiff(WORKSPACE_DIR, req.file || undefined);
    return create(GetGitDiffResponseSchema, { diff });
  },

  /**
   * Terminal establishes a bidirectional stream for PTY I/O
   */
  async *terminal(requests: AsyncIterable<TerminalInput>) {
    for await (const data of handleTerminalStream(requests)) {
      yield create(TerminalOutputSchema, { data });
    }
  },

  /**
   * Health returns the health status of the agent
   */
  async health() {
    return create(HealthResponseSchema, {
      healthy: true,
      version: "1.0.0",
    });
  },
};
