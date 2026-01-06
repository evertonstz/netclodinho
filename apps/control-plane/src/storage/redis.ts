/**
 * Redis Storage Module
 *
 * Handles persistence for sessions, messages, and events.
 *
 * Key structure:
 *   sessions:all                  SET      {id1, id2, ...}
 *   session:{id}                  HASH     {name, status, repo, createdAt, lastActiveAt}
 *   session:{id}:messages         LIST     [JSON strings]
 *   session:{id}:events           LIST     [JSON strings]
 */
import Redis from "ioredis";
import type { Session } from "@netclode/protocol";
import type { PersistedMessage, PersistedEvent } from "@netclode/protocol";
import { config } from "../config";

export class RedisStorage {
  private client: Redis;

  constructor(url?: string) {
    this.client = new Redis(url ?? config.redisUrl, {
      retryStrategy: (times) => {
        if (times > 10) return null;
        return Math.min(times * 100, 3000);
      },
      maxRetriesPerRequest: 3,
    });

    this.client.on("error", (err) => {
      console.error("[redis] Connection error:", err.message);
    });

    this.client.on("connect", () => {
      console.log("[redis] Connected");
    });
  }

  // === Key helpers ===

  private sessionsSetKey(): string {
    return "sessions:all";
  }

  private sessionKey(id: string): string {
    return `session:${id}`;
  }

  private messagesKey(id: string): string {
    return `session:${id}:messages`;
  }

  private eventsKey(id: string): string {
    return `session:${id}:events`;
  }

  // === Session operations ===

  async saveSession(session: Session): Promise<void> {
    const key = this.sessionKey(session.id);
    await this.client
      .multi()
      .sadd(this.sessionsSetKey(), session.id)
      .hset(key, {
        name: session.name,
        status: session.status,
        repo: session.repo ?? "",
        createdAt: session.createdAt,
        lastActiveAt: session.lastActiveAt,
      })
      .exec();
  }

  async getSession(id: string): Promise<Session | null> {
    const data = await this.client.hgetall(this.sessionKey(id));
    if (!data || Object.keys(data).length === 0) return null;

    return {
      id,
      name: data.name,
      status: data.status as Session["status"],
      repo: data.repo || undefined,
      createdAt: data.createdAt,
      lastActiveAt: data.lastActiveAt,
    };
  }

  async getAllSessions(): Promise<Session[]> {
    const ids = await this.client.smembers(this.sessionsSetKey());
    if (ids.length === 0) return [];

    const pipeline = this.client.pipeline();
    for (const id of ids) {
      pipeline.hgetall(this.sessionKey(id));
    }

    const results = await pipeline.exec();
    if (!results) return [];

    const sessions: Session[] = [];
    for (let i = 0; i < ids.length; i++) {
      const [err, data] = results[i] as [Error | null, Record<string, string>];
      if (err || !data || Object.keys(data).length === 0) continue;

      sessions.push({
        id: ids[i],
        name: data.name,
        status: data.status as Session["status"],
        repo: data.repo || undefined,
        createdAt: data.createdAt,
        lastActiveAt: data.lastActiveAt,
      });
    }

    return sessions;
  }

  async deleteSession(id: string): Promise<void> {
    await this.client
      .multi()
      .srem(this.sessionsSetKey(), id)
      .del(this.sessionKey(id))
      .del(this.messagesKey(id))
      .del(this.eventsKey(id))
      .exec();
  }

  async updateSessionField(
    id: string,
    field: string,
    value: string
  ): Promise<void> {
    await this.client.hset(this.sessionKey(id), field, value);
  }

  async updateSessionFields(
    id: string,
    fields: Partial<Omit<Session, "id">>
  ): Promise<void> {
    const updates: Record<string, string> = {};
    if (fields.name !== undefined) updates.name = fields.name;
    if (fields.status !== undefined) updates.status = fields.status;
    if (fields.repo !== undefined) updates.repo = fields.repo;
    if (fields.createdAt !== undefined) updates.createdAt = fields.createdAt;
    if (fields.lastActiveAt !== undefined)
      updates.lastActiveAt = fields.lastActiveAt;

    if (Object.keys(updates).length > 0) {
      await this.client.hset(this.sessionKey(id), updates);
    }
  }

  // === Message operations ===

  async appendMessage(
    sessionId: string,
    message: PersistedMessage
  ): Promise<void> {
    const key = this.messagesKey(sessionId);
    await this.client.rpush(key, JSON.stringify(message));

    // Trim to max messages
    const len = await this.client.llen(key);
    if (len > config.maxMessagesPerSession) {
      await this.client.ltrim(key, len - config.maxMessagesPerSession, -1);
    }
  }

  async getMessages(
    sessionId: string,
    afterId?: string
  ): Promise<PersistedMessage[]> {
    const raw = await this.client.lrange(this.messagesKey(sessionId), 0, -1);
    const messages = raw.map((r) => JSON.parse(r) as PersistedMessage);

    if (afterId) {
      const idx = messages.findIndex((m) => m.id === afterId);
      if (idx !== -1) {
        return messages.slice(idx + 1);
      }
    }

    return messages;
  }

  async getMessageCount(sessionId: string): Promise<number> {
    return this.client.llen(this.messagesKey(sessionId));
  }

  async getLastMessage(sessionId: string): Promise<PersistedMessage | null> {
    const raw = await this.client.lrange(this.messagesKey(sessionId), -1, -1);
    if (raw.length === 0) return null;
    return JSON.parse(raw[0]) as PersistedMessage;
  }

  // === Event operations ===

  async appendEvent(sessionId: string, event: PersistedEvent): Promise<void> {
    const key = this.eventsKey(sessionId);
    await this.client.rpush(key, JSON.stringify(event));

    // Trim to max events
    const len = await this.client.llen(key);
    if (len > config.maxEventsPerSession) {
      await this.client.ltrim(key, len - config.maxEventsPerSession, -1);
    }
  }

  async getEvents(
    sessionId: string,
    limit?: number
  ): Promise<PersistedEvent[]> {
    const maxLimit = limit ?? config.maxEventsPerSession;
    const raw = await this.client.lrange(
      this.eventsKey(sessionId),
      -maxLimit,
      -1
    );
    return raw.map((r) => JSON.parse(r) as PersistedEvent);
  }

  async clearEvents(sessionId: string): Promise<void> {
    await this.client.del(this.eventsKey(sessionId));
  }

  // === Health ===

  async ping(): Promise<boolean> {
    try {
      const result = await this.client.ping();
      return result === "PONG";
    } catch {
      return false;
    }
  }

  async close(): Promise<void> {
    await this.client.quit();
  }
}

// Singleton instance
let instance: RedisStorage | null = null;

export function getRedisStorage(): RedisStorage {
  if (!instance) {
    instance = new RedisStorage();
  }
  return instance;
}

export async function initRedisStorage(): Promise<RedisStorage> {
  console.log("[redis] Initializing Redis storage...");
  const storage = getRedisStorage();
  console.log("[redis] Pinging Redis...");
  const ok = await storage.ping();
  if (!ok) {
    console.error("[redis] Ping failed!");
    throw new Error("Failed to connect to Redis");
  }
  console.log("[redis] Ping successful");
  return storage;
}
