import type { ServerWebSocket } from "bun";
import type { ClientMessage, ServerMessage } from "@netclode/protocol";
import type { SessionManager } from "../sessions/manager";
import { config } from "../config";

interface WSData {
  sessionManager: SessionManager;
  subscriptions: Map<string, () => void>;
}

export function createWebSocketServer(sessionManager: SessionManager) {
  return {
    open(ws: ServerWebSocket<WSData>) {
      console.log("[ws] Client connected");
    },

    close(ws: ServerWebSocket<WSData>) {
      // Clean up subscriptions
      const subCount = ws.data.subscriptions.size;
      for (const unsub of ws.data.subscriptions.values()) {
        unsub();
      }
      console.log(`[ws] Client disconnected (had ${subCount} subscriptions)`);
    },

    async message(ws: ServerWebSocket<WSData>, message: string | Buffer) {
      const raw = typeof message === "string" ? message : message.toString();
      console.log(`[ws] Received: ${raw.slice(0, 200)}${raw.length > 200 ? "..." : ""}`);
      try {
        const data = JSON.parse(raw) as ClientMessage;

        await handleMessage(data, sessionManager, ws);
      } catch (error) {
        console.error("[ws] Error handling message:", error);
        const errorResponse: ServerMessage = {
          type: "error",
          message: error instanceof Error ? error.message : "Unknown error",
        };
        ws.send(JSON.stringify(errorResponse));
      }
    },
  };
}

async function handleMessage(
  message: ClientMessage,
  sessionManager: SessionManager,
  ws: ServerWebSocket<WSData>
): Promise<void> {
  const send = (msg: ServerMessage) => {
    console.log(`[ws] Sending: ${msg.type}`);
    ws.send(JSON.stringify(msg));
  };

  console.log(`[ws] Handling message type: ${message.type}`);

  switch (message.type) {
    case "session.create": {
      console.log(`[ws] Creating session: name=${message.name}, repo=${message.repo}`);
      const session = await sessionManager.create({
        name: message.name,
        repo: message.repo,
      });
      console.log(`[ws] Session created: ${session.id}`);

      // Auto-subscribe to agent messages
      if (!ws.data.subscriptions.has(session.id)) {
        const unsub = sessionManager.subscribe(session.id, (msg) => {
          console.log(`[ws] Forwarding to client: ${msg.type} for session ${session.id}`);
          ws.send(JSON.stringify(msg));
        });
        ws.data.subscriptions.set(session.id, unsub);
        console.log(`[ws] Subscribed to session ${session.id}`);
      }

      send({ type: "session.created", session });
      break;
    }

    case "session.list": {
      console.log("[ws] Listing sessions");
      const sessions = await sessionManager.list();
      console.log(`[ws] Found ${sessions.length} sessions`);
      send({ type: "session.list", sessions });
      break;
    }

    case "session.resume": {
      console.log(`[ws] Resuming session: ${message.id}`);
      const session = await sessionManager.resume(message.id);
      console.log(`[ws] Session resumed: ${session.id}, status=${session.status}`);

      // Subscribe to agent messages for this session
      if (!ws.data.subscriptions.has(message.id)) {
        const unsub = sessionManager.subscribe(message.id, (msg) => {
          console.log(`[ws] Forwarding to client: ${msg.type} for session ${message.id}`);
          ws.send(JSON.stringify(msg));
        });
        ws.data.subscriptions.set(message.id, unsub);
        console.log(`[ws] Subscribed to session ${message.id}`);
      }

      send({ type: "session.updated", session });
      break;
    }

    case "session.pause": {
      console.log(`[ws] Pausing session: ${message.id}`);
      const session = await sessionManager.pause(message.id);

      // Unsubscribe from agent messages
      const unsub = ws.data.subscriptions.get(message.id);
      if (unsub) {
        unsub();
        ws.data.subscriptions.delete(message.id);
        console.log(`[ws] Unsubscribed from session ${message.id}`);
      }

      send({ type: "session.updated", session });
      break;
    }

    case "session.delete": {
      console.log(`[ws] Deleting session: ${message.id}`);
      // Unsubscribe first
      const unsub = ws.data.subscriptions.get(message.id);
      if (unsub) {
        unsub();
        ws.data.subscriptions.delete(message.id);
      }

      await sessionManager.delete(message.id);
      send({ type: "session.deleted", id: message.id });
      break;
    }

    case "prompt": {
      console.log(`[ws] Sending prompt to session ${message.sessionId}: "${message.text.slice(0, 50)}..."`);
      // Fire and forget - responses come via subscription
      sessionManager.sendPrompt(message.sessionId, message.text).catch((error) => {
        console.error(`[ws] Prompt error for session ${message.sessionId}:`, error);
        send({
          type: "agent.error",
          sessionId: message.sessionId,
          error: error instanceof Error ? error.message : "Failed to send prompt",
        });
      });
      break;
    }

    case "prompt.interrupt": {
      console.log(`[ws] Interrupting session ${message.sessionId}`);
      sessionManager.interrupt(message.sessionId);
      break;
    }

    case "terminal.input": {
      console.log(`[ws] Terminal input for session ${message.sessionId}: ${message.data}`);
      break;
    }

    case "terminal.resize": {
      console.log(`[ws] Terminal resize for session ${message.sessionId}: ${message.cols}x${message.rows}`);
      break;
    }

    case "sync": {
      console.log("[ws] Sync request");
      const sessions = await sessionManager.getAllSessionsWithMeta();
      console.log(`[ws] Sync response: ${sessions.length} sessions`);
      send({
        type: "sync.response",
        sessions,
        serverTime: new Date().toISOString(),
      });
      break;
    }

    case "session.open": {
      console.log(`[ws] Opening session: ${message.id}, lastMessageId=${message.lastMessageId}`);
      const result = await sessionManager.getSessionWithHistory(
        message.id,
        message.lastMessageId
      );

      if (!result) {
        console.log(`[ws] Session ${message.id} not found`);
        send({ type: "session.error", id: message.id, error: "Session not found" });
        break;
      }

      console.log(`[ws] Session ${message.id} found: ${result.messages.length} messages, ${result.events.length} events`);

      // Auto-subscribe to session
      if (!ws.data.subscriptions.has(message.id)) {
        const unsub = sessionManager.subscribe(message.id, (msg) => {
          console.log(`[ws] Forwarding to client: ${msg.type} for session ${message.id}`);
          ws.send(JSON.stringify(msg));
        });
        ws.data.subscriptions.set(message.id, unsub);
        console.log(`[ws] Subscribed to session ${message.id}`);
      }

      send({
        type: "session.state",
        session: result.session,
        messages: result.messages,
        events: result.events,
        hasMore: result.messages.length >= config.maxMessagesPerSession,
      });
      break;
    }

    default:
      console.log(`[ws] Unknown message type: ${(message as { type: string }).type}`);
      send({ type: "error", message: "Unknown message type" });
  }
}
