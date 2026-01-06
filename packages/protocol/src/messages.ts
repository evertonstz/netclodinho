import type { Session } from "./session";
import type { AgentEvent } from "./events";
import type { PersistedMessage, PersistedEvent, SessionWithMeta } from "./storage";

// Client -> Server messages
export type ClientMessage =
  | { type: "session.create"; name?: string; repo?: string }
  | { type: "session.list" }
  | { type: "session.resume"; id: string }
  | { type: "session.pause"; id: string }
  | { type: "session.delete"; id: string }
  | { type: "prompt"; sessionId: string; text: string }
  | { type: "prompt.interrupt"; sessionId: string }
  | { type: "terminal.input"; sessionId: string; data: string }
  | { type: "terminal.resize"; sessionId: string; cols: number; rows: number }
  // Sync messages
  | { type: "sync" }
  | { type: "session.open"; id: string; lastMessageId?: string };

// Server -> Client messages
export type ServerMessage =
  | { type: "session.created"; session: Session }
  | { type: "session.updated"; session: Session }
  | { type: "session.deleted"; id: string }
  | { type: "session.list"; sessions: Session[] }
  | { type: "session.error"; id?: string; error: string }
  | { type: "terminal.output"; sessionId: string; data: string }
  | { type: "agent.event"; sessionId: string; event: AgentEvent }
  | {
      type: "agent.message";
      sessionId: string;
      content: string;
      partial?: boolean;
      messageId?: string;
    }
  | { type: "agent.done"; sessionId: string }
  | { type: "agent.error"; sessionId: string; error: string }
  | { type: "error"; message: string }
  // Sync responses
  | { type: "sync.response"; sessions: SessionWithMeta[]; serverTime: string }
  | {
      type: "session.state";
      session: Session;
      messages: PersistedMessage[];
      events: PersistedEvent[];
      hasMore: boolean;
    };

export type WSMessage = ClientMessage | ServerMessage;
