/**
 * Session Manager
 *
 * Manages Claude Code agent sessions using Kubernetes and agent-sandbox
 */
import type {
  AgentEvent,
  Session,
  SessionCreateRequest,
  ServerMessage,
} from "@netclode/protocol";
import { KubernetesRuntime } from "../runtime/kubernetes";
import { config } from "../config";

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
    const sandboxIds = new Set(sandboxes.map((s) => s.name.replace("sess-", "")));

    // Update statuses based on actual sandbox state
    for (const [id, state] of this.sessions) {
      if (!sandboxIds.has(id) && state.session.status === "running") {
        state.session.status = "paused";
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

    if (state.session.status === "creating" && sandboxInfo?.status !== "ready") {
      serviceFQDN = (await runtime.waitForReady(id, 120000)) ?? undefined;
      if (!serviceFQDN) {
        state.session.status = "error";
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
        this.emitSession(state, {
          type: "session.updated",
          session: state.session,
        });
        throw new Error("Failed to resume session");
      }
    }

    state.session.status = "running";
    state.session.lastActiveAt = new Date().toISOString();

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

    this.sessions.delete(id);
  }

  /**
   * Send a prompt to the agent and stream responses
   */
  async sendPrompt(id: string, text: string): Promise<void> {
    const state = this.sessions.get(id);
    if (!state) {
      throw new Error(`Session ${id} not found`);
    }

    if (!state.agent?.connected || !state.agent.serviceFQDN) {
      throw new Error(`Session ${id} agent not connected`);
    }

    state.session.lastActiveAt = new Date().toISOString();
    state.session.status = "running";

    const agentUrl = `http://${state.agent.serviceFQDN}:3002`;

    try {
      // Make HTTP request to agent
      const response = await fetch(`${agentUrl}/prompt`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ sessionId: id, text }),
      });

      if (!response.ok) {
        throw new Error(`Agent returned ${response.status}`);
      }

      // Stream SSE response
      const reader = response.body?.getReader();
      if (!reader) {
        throw new Error("No response body");
      }

      const decoder = new TextDecoder();
      let buffer = "";

      const emit = (message: ServerMessage) => {
        for (const handler of state.messageHandlers) {
          handler(message);
        }
      };

      const handleSseData = (data: unknown) => {
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
            const content = typeof payload.content === "string" ? payload.content : "";
            const partial = typeof payload.partial === "boolean" ? payload.partial : undefined;
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
              emit({
                type: "agent.event",
                sessionId: id,
                event: payload.event as AgentEvent,
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
            handleSseData(JSON.parse(payload));
          } catch {
            emit({
              type: "agent.message",
              sessionId: id,
              content: payload,
            });
          }
        }
      }

      // Notify completion
      emit({ type: "agent.done", sessionId: id });

      state.session.status = "ready";
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
      console.log(`[${id}] Session running at ${serviceFQDN}`);
      this.emitSession(state, {
        type: "session.updated",
        session: state.session,
      });
    } catch (error) {
      state.session.status = "error";
      this.emitSession(state, {
        type: "session.updated",
        session: state.session,
      });
      this.emitSession(state, {
        type: "session.error",
        id,
        error: error instanceof Error ? error.message : "Failed to create session",
      });
      throw error;
    }
  }

  // TODO: Implement snapshots using Kubernetes VolumeSnapshots
  // async createSnapshot(id: string, name: string): Promise<void> { }
  // async restoreSnapshot(id: string, name: string): Promise<void> { }
  // async listSnapshots(id: string): Promise<string[]> { }
}
