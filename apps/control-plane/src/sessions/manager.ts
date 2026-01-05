/**
 * Session Manager
 *
 * Manages Claude Code agent sessions using containerd/nerdctl
 */
import type { Session, SessionCreateRequest, ServerMessage } from "@netclode/protocol";
import * as runtime from "../runtime/nerdctl";
import * as storage from "../storage/juicefs";
import { config } from "../config";

export type MessageHandler = (message: ServerMessage) => void;

interface AgentConnection {
  ip: string;
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

    try {
      // Create workspace on JuiceFS
      console.log(`[${id}] Creating workspace...`);
      await storage.createWorkspace(id);

      // Clone repo if provided
      if (request.repo) {
        console.log(`[${id}] Cloning repo: ${request.repo}`);
        await storage.cloneRepo(id, request.repo);
      }

      // Create VM via nerdctl
      console.log(`[${id}] Creating VM...`);
      await runtime.createVM({
        sessionId: id,
        cpus: config.defaultCpus,
        memoryMB: config.defaultMemoryMB,
      });

      // Wait for VM to be ready and get its IP
      console.log(`[${id}] Waiting for VM to be ready...`);
      const ip = await runtime.waitForVMReady(id, 120000);
      if (!ip) {
        session.status = "error";
        throw new Error("VM failed to become ready");
      }

      session.status = "ready";
      console.log(`[${id}] Session ready at ${ip}`);
    } catch (e) {
      console.error(`[${id}] Failed to create session:`, e);
      session.status = "error";
      throw e;
    }

    return session;
  }

  async list(): Promise<Session[]> {
    // Sync with actual VM state
    const vms = await runtime.listVMs();
    const vmNames = new Set(vms.map((vm) => vm.name.replace("sess-", "")));

    // Update statuses based on actual VM state
    for (const [id, state] of this.sessions) {
      if (!vmNames.has(id) && state.session.status === "running") {
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

    // Check if VM is running and get IP
    let ip = await runtime.getVMIPAddress(id);

    if (!ip) {
      // Check if workspace exists
      const hasWorkspace = await storage.workspaceExists(id);
      if (!hasWorkspace) {
        throw new Error(`Session ${id} workspace not found`);
      }

      // Recreate VM
      console.log(`[${id}] VM not running, recreating...`);
      await runtime.createVM({
        sessionId: id,
        cpus: config.defaultCpus,
        memoryMB: config.defaultMemoryMB,
      });

      ip = await runtime.waitForVMReady(id, 120000);
      if (!ip) {
        state.session.status = "error";
        throw new Error("Failed to resume session");
      }
    }

    state.session.status = "running";
    state.session.lastActiveAt = new Date().toISOString();

    // Store agent connection
    state.agent = {
      ip,
      connected: true,
      close: () => {
        if (state.agent) state.agent.connected = false;
      },
    };

    console.log(`[${id}] Resumed, agent at ${ip}`);
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

    // Stop VM (keeps workspace)
    await runtime.stopVM(id);
    await runtime.removeVM(id);

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

    // Remove VM
    await runtime.removeVM(id);

    // Delete workspace
    await storage.deleteWorkspace(id);

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

    if (!state.agent?.connected || !state.agent.ip) {
      throw new Error(`Session ${id} agent not connected`);
    }

    state.session.lastActiveAt = new Date().toISOString();
    state.session.status = "running";

    const agentUrl = `http://${state.agent.ip}:3002`;

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

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });

        // Parse SSE events
        const lines = buffer.split("\n");
        buffer = lines.pop() || ""; // Keep incomplete line in buffer

        for (const line of lines) {
          if (line.startsWith("data: ")) {
            try {
              const data = JSON.parse(line.slice(6));
              for (const handler of state.messageHandlers) {
                handler({
                  type: "agent.event",
                  sessionId: id,
                  event: data,
                });
              }
            } catch {
              // Not valid JSON, might be a message
              for (const handler of state.messageHandlers) {
                handler({
                  type: "agent.message",
                  sessionId: id,
                  content: line.slice(6),
                });
              }
            }
          }
        }
      }

      // Notify completion
      for (const handler of state.messageHandlers) {
        handler({ type: "agent.done", sessionId: id });
      }

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
    if (!state?.agent?.connected || !state.agent.ip) return;

    try {
      await fetch(`http://${state.agent.ip}:3002/interrupt`, {
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

  /**
   * Create a snapshot of a session
   */
  async createSnapshot(id: string, name: string): Promise<void> {
    const state = this.sessions.get(id);
    if (!state) {
      throw new Error(`Session ${id} not found`);
    }

    await storage.createSnapshot(id, name);
  }

  /**
   * Restore a session from snapshot
   */
  async restoreSnapshot(id: string, name: string): Promise<void> {
    const state = this.sessions.get(id);
    if (!state) {
      throw new Error(`Session ${id} not found`);
    }

    // Pause session first if running
    if (state.session.status === "running") {
      await this.pause(id);
    }

    await storage.restoreSnapshot(id, name);
  }

  /**
   * List snapshots for a session
   */
  async listSnapshots(id: string): Promise<string[]> {
    return storage.listSnapshots(id);
  }
}
