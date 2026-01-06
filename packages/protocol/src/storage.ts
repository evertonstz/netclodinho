import type { AgentEvent } from "./events";
import type { Session } from "./session";

/**
 * Persisted message stored in Redis
 */
export interface PersistedMessage {
  id: string; // "msg_<uuid>"
  sessionId: string;
  role: "user" | "assistant";
  content: string;
  timestamp: string; // ISO 8601
}

/**
 * Persisted event stored in Redis
 */
export interface PersistedEvent {
  id: string; // "evt_<uuid>"
  sessionId: string;
  event: AgentEvent;
  timestamp: string; // ISO 8601
}

/**
 * Session with sync metadata
 */
export interface SessionWithMeta extends Session {
  messageCount?: number;
  lastMessageId?: string;
}
