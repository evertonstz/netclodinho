/**
 * Shared utility functions for SDK adapters
 */

import type { JsonObject } from "@bufbuild/protobuf";

/**
 * Map tool names from various SDKs to a normalized format (capitalized)
 */
export const TOOL_NAME_MAP: Record<string, string> = {
  read: "Read",
  write: "Write",
  edit: "Edit",
  glob: "Glob",
  grep: "Grep",
  bash: "Bash",
  webfetch: "WebFetch",
  todowrite: "TodoWrite",
  todoread: "TodoRead",
  task: "Task",
  codesearch: "CodeSearch",
  websearch: "WebSearch",
};

/**
 * Normalize tool names to a consistent capitalized format
 */
export function normalizeToolName(name: string): string {
  return TOOL_NAME_MAP[name.toLowerCase()] || name;
}

/**
 * Convert camelCase string to snake_case
 */
export function toSnakeCase(str: string): string {
  return str.replace(/[A-Z]/g, (letter) => `_${letter.toLowerCase()}`);
}

/**
 * Normalize tool input keys from camelCase to snake_case
 */
export function normalizeToolInput(
  input: Record<string, unknown> | undefined
): Record<string, unknown> | undefined {
  if (!input) return undefined;

  const normalized: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(input)) {
    normalized[toSnakeCase(key)] = value;
  }
  return normalized;
}

/**
 * Generate a unique thinking ID with timestamp and counter
 */
export function generateThinkingId(counter: number): string {
  return `thinking_${Date.now()}_${counter}`;
}

/**
 * Generate a unique message ID with timestamp and counter
 */
export function generateMessageId(counter: number): string {
  return `msg_${Date.now()}_${counter}`;
}

/**
 * Parse tool JSON input safely, returning a fallback on failure
 */
export function parseToolInput(jsonString: string | undefined): JsonObject {
  if (!jsonString) return {};
  try {
    return JSON.parse(jsonString) as JsonObject;
  } catch {
    return { _raw: jsonString };
  }
}

/**
 * Calculate duration in milliseconds from a start time
 */
export function calculateDuration(startTime: number | undefined): number | undefined {
  return startTime !== undefined ? Date.now() - startTime : undefined;
}
