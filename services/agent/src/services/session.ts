/**
 * Session management - maps control-plane session IDs to Claude SDK session IDs
 * and persists the mapping to survive pod restarts.
 */

import { existsSync, readFileSync, writeFileSync, mkdirSync } from "fs";
import { dirname } from "path";

const SESSION_MAPPING_FILE = "/agent/.session-mapping.json";

// In-memory session map
const sessionMap = new Map<string, string>();

// Track initialization state per session
let initializedSessionId: string | null = null;

/**
 * Load session mapping from disk (survives pod restarts)
 */
export function loadSessionMapping(): void {
  try {
    if (existsSync(SESSION_MAPPING_FILE)) {
      const data = JSON.parse(readFileSync(SESSION_MAPPING_FILE, "utf-8"));
      const entries = Object.entries(data) as [string, string][];
      for (const [key, value] of entries) {
        sessionMap.set(key, value);
      }
      console.log(`[session] Loaded ${sessionMap.size} session mappings from ${SESSION_MAPPING_FILE}`);
    }
  } catch (err) {
    console.error(`[session] Failed to load session mapping:`, err);
  }
}

/**
 * Save session mapping to disk
 */
function saveSessionMapping(): void {
  try {
    const dir = dirname(SESSION_MAPPING_FILE);
    if (!existsSync(dir)) {
      mkdirSync(dir, { recursive: true });
    }
    writeFileSync(SESSION_MAPPING_FILE, JSON.stringify(Object.fromEntries(sessionMap), null, 2));
    console.log(`[session] Saved ${sessionMap.size} session mappings to ${SESSION_MAPPING_FILE}`);
  } catch (err) {
    console.error(`[session] Failed to save session mapping:`, err);
  }
}

/**
 * Get SDK session ID for a control-plane session ID
 */
export function getSdkSessionId(sessionId: string): string | undefined {
  return sessionMap.get(sessionId);
}

/**
 * Register a new session mapping
 */
export function registerSession(controlPlaneSessionId: string, sdkSessionId: string): void {
  if (!sessionMap.has(controlPlaneSessionId)) {
    sessionMap.set(controlPlaneSessionId, sdkSessionId);
    saveSessionMapping();
    console.log(`[session] Registered session mapping: ${controlPlaneSessionId} -> ${sdkSessionId}`);
  }
}

/**
 * Check if a session has been initialized (repo cloned, etc.)
 */
export function isSessionInitialized(sessionId: string): boolean {
  return initializedSessionId === sessionId;
}

/**
 * Mark a session as initialized
 */
export function markSessionInitialized(sessionId: string): void {
  initializedSessionId = sessionId;
  console.log(`[session] Marked session ${sessionId} as initialized`);
}

/**
 * Get the currently initialized session ID
 */
export function getInitializedSessionId(): string | null {
  return initializedSessionId;
}

// Load session mapping on module initialization
loadSessionMapping();
