/**
 * Session Manager
 *
 * Manages Claude Code agent sessions using Kubernetes and agent-sandbox.
 * Persists session state, messages, and events to Redis.
 */
import type {
  AgentEvent,
  Session,
  SessionCreateRequest,
  ServerMessage,
  PersistedMessage,
  PersistedEvent,
  SessionWithMeta,
} from "@netclode/protocol";
import { KubernetesRuntime } from "../runtime/kubernetes";
import { config } from "../config";
import type { RedisStorage } from "../storage/redis";

// Kubernetes runtime instance
const runtime = new KubernetesRuntime();

export type MessageHandler = (message: ServerMessage) => void;

interface AgentConnection {
  serviceFQDN: string;
  connected: boolean;
  close: () => void;
}

interface SessionState {
  session: Session;
  agent: AgentConnection | null;
  messageHandlers: Set<MessageHandler>;
}

export class SessionManager {
  private sessions: Map<string, SessionState> = new Map();
  private storage: RedisStorage;
  private initialized = false;

  constructor(storage: RedisStorage) {
    this.storage = storage;
  }

  /**
   * Initialize the session manager by loading sessions from Redis
   * and reconciling with K8s state.
   */
  async initialize(): Promise<void> {
    if (this.initialized) return;

    console.log("[sessions] Loading sessions from Redis...");
    await this.loadFromRedis();

    console.log("[sessions] Reconciling with K8s state...");
    await this.reconcileWithK8s();

    this.initialized = true;
    console.log(`[sessions] Initialized with ${this.sessions.size} sessions`);
  }

  private async loadFromRedis(): Promise<void> {
    const sessions = await this.storage.getAllSessions();
    for (const session of sessions) {
      this.sessions.set(session.id, {
        session,
        agent: null,
        messageHandlers: new Set(),
      });
    }
  }

  private async reconcileWithK8s(): Promise<void> {
    const sandboxes = await runtime.listSandboxes();
    const sandboxMap = new Map(
      sandboxes.map((s) => [s.name.replace("sess-", ""), s])
    );

    for (const [id, state] of this.sessions) {
      const sandbox = sandboxMap.get(id);

      if (!sandbox) {
        // No sandbox - mark as paused if currently running
        if (
          state.session.status === "running" ||
          state.session.status === "creating"
        ) {
          state.session.status = "paused";
          await this.storage.updateSessionField(id, "status", "paused");
          console.log(`[${id}] Reconciled: no sandbox, marked paused`);
        }
      } else if (sandbox.status === "ready") {
        // Sandbox ready - could reconnect
        if (state.session.status === "paused") {
          // Don't auto-resume, just note it's available
          console.log(`[${id}] Reconciled: sandbox ready, session paused`);
        }
      }
    }
  }

  async create(request: SessionCreateRequest): Promise<Session> {
    const id = crypto.randomUUID().slice(0, 12);
    const session: Session = {
      id,
      name: request.name || `session-${id.slice(0, 6)}`,
      status: "creating",
      repo: request.repo,
      createdAt: new Date().toISOString(),
      lastActiveAt: new Date().toISOString(),
    };

    // Persist to Redis first
    await this.storage.saveSession(session);

    this.sessions.set(id, {
      session,
      agent: null,
      messageHandlers: new Set(),
    });

    this.startSessionCreation(id, request).catch((error) => {
      console.error(`[${id}] Failed to create session:`, error);
    });

    return session;
  }

  async list(): Promise<Session[]> {
    // Sync with actual sandbox state
    const sandboxes = await runtime.listSandboxes();
    const sandboxIds = new Set(
      sandboxes.map((s) => s.name.replace("sess-", ""))
    );

    // Update statuses based on actual sandbox state
    for (const [id, state] of this.sessions) {
      if (!sandboxIds.has(id) && state.session.status === "running") {
        state.session.status = "paused";
        // Persist status change
        await this.storage.updateSessionField(id, "status", "paused");
      }
    }

    return Array.from(this.sessions.values()).map((s) => s.session);
  }

  async get(id: string): Promise<Session | undefined> {
    return this.sessions.get(id)?.session;
  }

  async resume(id: string): Promise<Session> {
    const state = this.sessions.get(id);
    if (!state) {
      throw new Error(`Session ${id} not found`);
    }

    // Check if sandbox is running
    let sandboxInfo = await runtime.getSandboxStatus(id);
    let serviceFQDN = sandboxInfo?.serviceFQDN;

    if (
      state.session.status === "creating" &&
      sandboxInfo?.status !== "ready"
    ) {
      serviceFQDN = (await runtime.waitForReady(id, 120000)) ?? undefined;
      if (!serviceFQDN) {
        state.session.status = "error";
        await this.storage.updateSessionField(id, "status", "error");
        this.emitSession(state, {
          type: "session.updated",
          session: state.session,
        });
        throw new Error("Failed to resume session");
      }
    } else if (!serviceFQDN || sandboxInfo?.status !== "ready") {
      // Recreate sandbox
      console.log(`[${id}] Sandbox not running, recreating...`);

      // Delete old sandbox if exists
      if (sandboxInfo) {
        await runtime.deleteSandbox(id);
      }

      await runtime.createSandbox({
        sessionId: id,
        cpus: config.defaultCpus,
        memoryMB: config.defaultMemoryMB,
      });

      serviceFQDN = (await runtime.waitForReady(id, 120000)) ?? undefined;
      if (!serviceFQDN) {
        state.session.status = "error";
        await this.storage.updateSessionField(id, "status", "error");
        this.emitSession(state, {
          type: "session.updated",
          session: state.session,
        });
        throw new Error("Failed to resume session");
      }
    }

    state.session.status = "running";
    state.session.lastActiveAt = new Date().toISOString();

    // Persist status change
    await this.storage.updateSessionFields(id, {
      status: "running",
      lastActiveAt: state.session.lastActiveAt,
    });

    // Store agent connection
    state.agent = {
      serviceFQDN,
      connected: true,
      close: () => {
        if (state.agent) state.agent.connected = false;
      },
    };

    console.log(`[${id}] Resumed, agent at ${serviceFQDN}`);
    return state.session;
  }

  async pause(id: string): Promise<Session> {
    const state = this.sessions.get(id);
    if (!state) {
      throw new Error(`Session ${id} not found`);
    }

    // Disconnect agent
    if (state.agent) {
      state.agent.close();
      state.agent = null;
    }

    // Delete sandbox (PVC persists)
    await runtime.deleteSandbox(id);

    state.session.status = "paused";
    state.session.lastActiveAt = new Date().toISOString();

    // Persist status change
    await this.storage.updateSessionFields(id, {
      status: "paused",
      lastActiveAt: state.session.lastActiveAt,
    });

    return state.session;
  }

  async delete(id: string): Promise<void> {
    const state = this.sessions.get(id);
    if (!state) {
      throw new Error(`Session ${id} not found`);
    }

    // Disconnect agent
    if (state.agent) {
      state.agent.close();
    }

    // Delete sandbox and associated resources
    await runtime.deleteSandbox(id);

    // Remove from Redis
    await this.storage.deleteSession(id);

    this.sessions.delete(id);
  }

  /**
   * Send a prompt to the agent and stream responses.
   * Persists user message and final assistant response to Redis.
   */
  async sendPrompt(id: string, text: string): Promise<void> {
    console.log(`[${id}] sendPrompt called: "${text.slice(0, 50)}..."`);

    const state = this.sessions.get(id);
    if (!state) {
      console.error(`[${id}] sendPrompt: Session not found in memory`);
      throw new Error(`Session ${id} not found`);
    }

    console.log(`[${id}] sendPrompt: agent connected=${state.agent?.connected}, fqdn=${state.agent?.serviceFQDN}`);
    if (!state.agent?.connected || !state.agent.serviceFQDN) {
      console.error(`[${id}] sendPrompt: Agent not connected`);
      throw new Error(`Session ${id} agent not connected`);
    }

    state.session.lastActiveAt = new Date().toISOString();
    state.session.status = "running";

    // Persist user message
    const userMessageId = `msg_${crypto.randomUUID().slice(0, 12)}`;
    console.log(`[${id}] sendPrompt: Persisting user message ${userMessageId}`);
    await this.storage.appendMessage(id, {
      id: userMessageId,
      sessionId: id,
      role: "user",
      content: text,
      timestamp: new Date().toISOString(),
    });

    const agentUrl = `http://${state.agent.serviceFQDN}:3002`;
    console.log(`[${id}] sendPrompt: Sending to agent at ${agentUrl}`);

    // Accumulate assistant content for final persistence
    let assistantContent = "";

    try {
      // Make HTTP request to agent
      console.log(`[${id}] sendPrompt: Fetching ${agentUrl}/prompt`);
      const response = await fetch(`${agentUrl}/prompt`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ sessionId: id, text }),
      });

      console.log(`[${id}] sendPrompt: Agent response status=${response.status}`);
      if (!response.ok) {
        throw new Error(`Agent returned ${response.status}`);
      }

      // Stream SSE response
      const reader = response.body?.getReader();
      if (!reader) {
        throw new Error("No response body");
      }
      console.log(`[${id}] sendPrompt: Starting SSE stream`);

      const decoder = new TextDecoder();
      let buffer = "";

      const emit = (message: ServerMessage) => {
        console.log(`[${id}] sendPrompt: Emitting ${message.type} to ${state.messageHandlers.size} handlers`);
        for (const handler of state.messageHandlers) {
          handler(message);
        }
      };

      const handleSseData = async (data: unknown) => {
        if (!data || typeof data !== "object") return;
        const payload = data as {
          type?: string;
          content?: unknown;
          partial?: unknown;
          event?: unknown;
          error?: unknown;
          message?: unknown;
        };

        switch (payload.type) {
          case "agent.message": {
            const content =
              typeof payload.content === "string" ? payload.content : "";
            const partial =
              typeof payload.partial === "boolean" ? payload.partial : undefined;

            if (partial) {
              // Accumulate partial content
              assistantContent += content;
            } else {
              // Full message - use as final content
              assistantContent = content;
            }

            emit({
              type: "agent.message",
              sessionId: id,
              content,
              ...(partial !== undefined ? { partial } : {}),
            });
            return;
          }
          case "agent.event": {
            if (payload.event) {
              const event = payload.event as AgentEvent;

              // Persist event to Redis
              const eventId = `evt_${crypto.randomUUID().slice(0, 12)}`;
              await this.storage.appendEvent(id, {
                id: eventId,
                sessionId: id,
                event,
                timestamp: new Date().toISOString(),
              });

              emit({
                type: "agent.event",
                sessionId: id,
                event,
              });
            }
            return;
          }
          case "error":
          case "agent.error": {
            const errorMessage =
              typeof payload.error === "string"
                ? payload.error
                : typeof payload.message === "string"
                  ? payload.message
                  : "Unknown error";
            emit({
              type: "agent.error",
              sessionId: id,
              error: errorMessage,
            });
            return;
          }
          case "agent.system":
          case "agent.result":
          case "start":
          case "done":
          default:
            return;
        }
      };

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });

        // Parse SSE events
        const lines = buffer.split("\n");
        buffer = lines.pop() || ""; // Keep incomplete line in buffer

        for (const line of lines) {
          if (!line.startsWith("data: ")) continue;
          const payload = line.slice(6);
          try {
            await handleSseData(JSON.parse(payload));
          } catch {
            emit({
              type: "agent.message",
              sessionId: id,
              content: payload,
            });
          }
        }
      }

      // Persist final assistant message
      if (assistantContent) {
        const assistantMessageId = `msg_${crypto.randomUUID().slice(0, 12)}`;
        await this.storage.appendMessage(id, {
          id: assistantMessageId,
          sessionId: id,
          role: "assistant",
          content: assistantContent,
          timestamp: new Date().toISOString(),
        });

        // Emit done with messageId
        emit({ type: "agent.done", sessionId: id });
      } else {
        emit({ type: "agent.done", sessionId: id });
      }

      state.session.status = "ready";
      await this.storage.updateSessionField(id, "status", "ready");
    } catch (error) {
      console.error(`[${id}] Prompt error:`, error);
      for (const handler of state.messageHandlers) {
        handler({
          type: "agent.error",
          sessionId: id,
          error: error instanceof Error ? error.message : "Unknown error",
        });
      }
      state.session.status = "ready";
      await this.storage.updateSessionField(id, "status", "ready");
      throw error;
    }
  }

  /**
   * Interrupt the current agent operation
   */
  async interrupt(id: string): Promise<void> {
    const state = this.sessions.get(id);
    if (!state?.agent?.connected || !state.agent.serviceFQDN) return;

    try {
      await fetch(`http://${state.agent.serviceFQDN}:3002/interrupt`, {
        method: "POST",
        signal: AbortSignal.timeout(5000),
      });
    } catch (error) {
      console.error(`[${id}] Interrupt error:`, error);
    }
  }

  subscribe(id: string, handler: MessageHandler): () => void {
    const state = this.sessions.get(id);
    if (!state) {
      throw new Error(`Session ${id} not found`);
    }

    state.messageHandlers.add(handler);
    return () => state.messageHandlers.delete(handler);
  }

  // === Sync Methods ===

  /**
   * Get all sessions with metadata for sync
   */
  async getAllSessionsWithMeta(): Promise<SessionWithMeta[]> {
    const sessions = await this.list();
    const results: SessionWithMeta[] = [];

    for (const session of sessions) {
      const messageCount = await this.storage.getMessageCount(session.id);
      const lastMessage = await this.storage.getLastMessage(session.id);

      results.push({
        ...session,
        messageCount,
        lastMessageId: lastMessage?.id,
      });
    }

    return results;
  }

  /**
   * Get session with full history for sync
   */
  async getSessionWithHistory(
    id: string,
    afterMessageId?: string
  ): Promise<{
    session: Session;
    messages: PersistedMessage[];
    events: PersistedEvent[];
  } | null> {
    const state = this.sessions.get(id);
    if (!state) return null;

    const messages = await this.storage.getMessages(id, afterMessageId);
    const events = await this.storage.getEvents(id);

    return {
      session: state.session,
      messages,
      events,
    };
  }

  private emitSession(state: SessionState, message: ServerMessage): void {
    for (const handler of state.messageHandlers) {
      handler(message);
    }
  }

  private async startSessionCreation(
    id: string,
    request: SessionCreateRequest
  ): Promise<void> {
    const state = this.sessions.get(id);
    if (!state) return;

    try {
      // Create sandbox via Kubernetes
      console.log(`[${id}] Creating sandbox...`);
      await runtime.createSandbox({
        sessionId: id,
        cpus: config.defaultCpus,
        memoryMB: config.defaultMemoryMB,
        env: request.repo ? { GIT_REPO: request.repo } : undefined,
      });

      // Wait for sandbox to be ready
      console.log(`[${id}] Waiting for sandbox to be ready...`);
      const serviceFQDN = await runtime.waitForReady(id, 120000);
      if (!serviceFQDN) {
        state.session.status = "error";
        await this.storage.updateSessionField(id, "status", "error");
        this.emitSession(state, {
          type: "session.updated",
          session: state.session,
        });
        this.emitSession(state, {
          type: "session.error",
          id,
          error: "Sandbox failed to become ready",
        });
        return;
      }

      state.agent = {
        serviceFQDN,
        connected: true,
        close: () => {
          if (state.agent) state.agent.connected = false;
        },
      };

      state.session.status = "running";
      state.session.lastActiveAt = new Date().toISOString();

      // Persist status change
      await this.storage.updateSessionFields(id, {
        status: "running",
        lastActiveAt: state.session.lastActiveAt,
      });

      console.log(`[${id}] Session running at ${serviceFQDN}`);
      this.emitSession(state, {
        type: "session.updated",
        session: state.session,
      });
    } catch (error) {
      state.session.status = "error";
      await this.storage.updateSessionField(id, "status", "error");
      this.emitSession(state, {
        type: "session.updated",
        session: state.session,
      });
      this.emitSession(state, {
        type: "session.error",
        id,
        error:
          error instanceof Error ? error.message : "Failed to create session",
      });
      throw error;
    }
  }

  // TODO: Implement snapshots using Kubernetes VolumeSnapshots
  // async createSnapshot(id: string, name: string): Promise<void> { }
  // async restoreSnapshot(id: string, name: string): Promise<void> { }
  // async listSnapshots(id: string): Promise<string[]> { }
}
