/**
 * Tests for SDK utility functions
 */

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  normalizeToolName,
  toSnakeCase,
  normalizeToolInput,
  generateThinkingId,
  generateMessageId,
  parseToolInput,
  calculateDuration,
  TOOL_NAME_MAP,
} from "./index.js";

describe("SDK Utils", () => {
  describe("normalizeToolName", () => {
    it("normalizes lowercase tool names to capitalized versions", () => {
      expect(normalizeToolName("read")).toBe("Read");
      expect(normalizeToolName("write")).toBe("Write");
      expect(normalizeToolName("edit")).toBe("Edit");
      expect(normalizeToolName("bash")).toBe("Bash");
      expect(normalizeToolName("glob")).toBe("Glob");
      expect(normalizeToolName("grep")).toBe("Grep");
    });

    it("handles mixed case input by lowercasing first", () => {
      expect(normalizeToolName("READ")).toBe("Read");
      expect(normalizeToolName("Write")).toBe("Write");
      expect(normalizeToolName("BASH")).toBe("Bash");
    });

    it("normalizes web-related tools", () => {
      expect(normalizeToolName("webfetch")).toBe("WebFetch");
      expect(normalizeToolName("codesearch")).toBe("CodeSearch");
      expect(normalizeToolName("websearch")).toBe("WebSearch");
    });

    it("normalizes task management tools", () => {
      expect(normalizeToolName("todowrite")).toBe("TodoWrite");
      expect(normalizeToolName("todoread")).toBe("TodoRead");
      expect(normalizeToolName("task")).toBe("Task");
    });

    it("returns original name for unknown tools", () => {
      expect(normalizeToolName("unknownTool")).toBe("unknownTool");
      expect(normalizeToolName("CustomTool")).toBe("CustomTool");
      expect(normalizeToolName("mcp_server_tool")).toBe("mcp_server_tool");
    });

    it("handles empty string", () => {
      expect(normalizeToolName("")).toBe("");
    });
  });

  describe("toSnakeCase", () => {
    it("converts camelCase to snake_case", () => {
      expect(toSnakeCase("camelCase")).toBe("camel_case");
      expect(toSnakeCase("myVariableName")).toBe("my_variable_name");
      expect(toSnakeCase("XMLParser")).toBe("_x_m_l_parser");
    });

    it("handles single words", () => {
      expect(toSnakeCase("word")).toBe("word");
      expect(toSnakeCase("Word")).toBe("_word");
    });

    it("handles already snake_case strings", () => {
      expect(toSnakeCase("already_snake_case")).toBe("already_snake_case");
    });

    it("handles empty string", () => {
      expect(toSnakeCase("")).toBe("");
    });

    it("handles common SDK parameter names", () => {
      expect(toSnakeCase("filePath")).toBe("file_path");
      expect(toSnakeCase("workingDirectory")).toBe("working_directory");
      expect(toSnakeCase("maxTokens")).toBe("max_tokens");
      expect(toSnakeCase("toolUseId")).toBe("tool_use_id");
    });
  });

  describe("normalizeToolInput", () => {
    it("converts camelCase keys to snake_case", () => {
      const input = {
        filePath: "/path/to/file",
        maxTokens: 1000,
        workingDirectory: "/workspace",
      };
      const result = normalizeToolInput(input);
      expect(result).toEqual({
        file_path: "/path/to/file",
        max_tokens: 1000,
        working_directory: "/workspace",
      });
    });

    it("preserves values unchanged", () => {
      const input = {
        nested: { key: "value" },
        array: [1, 2, 3],
        boolean: true,
        number: 42,
      };
      const result = normalizeToolInput(input);
      expect(result).toEqual({
        nested: { key: "value" },
        array: [1, 2, 3],
        boolean: true,
        number: 42,
      });
    });

    it("returns undefined for undefined input", () => {
      expect(normalizeToolInput(undefined)).toBeUndefined();
    });

    it("handles empty object", () => {
      expect(normalizeToolInput({})).toEqual({});
    });

    it("handles keys that are already snake_case", () => {
      const input = { file_path: "/path", max_tokens: 100 };
      const result = normalizeToolInput(input);
      expect(result).toEqual({ file_path: "/path", max_tokens: 100 });
    });
  });

  describe("generateThinkingId", () => {
    it("generates unique IDs with counter", () => {
      const id1 = generateThinkingId(1);
      const id2 = generateThinkingId(2);

      expect(id1).toMatch(/^thinking_\d+_1$/);
      expect(id2).toMatch(/^thinking_\d+_2$/);
      expect(id1).not.toBe(id2);
    });

    it("includes timestamp", () => {
      const before = Date.now();
      const id = generateThinkingId(1);
      const after = Date.now();

      const parts = id.split("_");
      const timestamp = parseInt(parts[1], 10);
      expect(timestamp).toBeGreaterThanOrEqual(before);
      expect(timestamp).toBeLessThanOrEqual(after);
    });
  });

  describe("generateMessageId", () => {
    it("generates unique IDs with counter", () => {
      const id1 = generateMessageId(1);
      const id2 = generateMessageId(2);

      expect(id1).toMatch(/^msg_\d+_1$/);
      expect(id2).toMatch(/^msg_\d+_2$/);
      expect(id1).not.toBe(id2);
    });

    it("includes timestamp", () => {
      const before = Date.now();
      const id = generateMessageId(5);
      const after = Date.now();

      const parts = id.split("_");
      const timestamp = parseInt(parts[1], 10);
      expect(timestamp).toBeGreaterThanOrEqual(before);
      expect(timestamp).toBeLessThanOrEqual(after);
    });
  });

  describe("parseToolInput", () => {
    it("parses valid JSON", () => {
      const json = '{"command": "ls -la", "cwd": "/workspace"}';
      const result = parseToolInput(json);
      expect(result).toEqual({ command: "ls -la", cwd: "/workspace" });
    });

    it("returns empty object for undefined input", () => {
      expect(parseToolInput(undefined)).toEqual({});
    });

    it("returns empty object for empty string", () => {
      expect(parseToolInput("")).toEqual({});
    });

    it("returns fallback object for invalid JSON", () => {
      const invalid = '{"incomplete": ';
      const result = parseToolInput(invalid);
      expect(result).toEqual({ _raw: invalid });
    });

    it("handles nested JSON structures", () => {
      const json = '{"options": {"verbose": true}, "files": ["a.ts", "b.ts"]}';
      const result = parseToolInput(json);
      expect(result).toEqual({
        options: { verbose: true },
        files: ["a.ts", "b.ts"],
      });
    });
  });

  describe("calculateDuration", () => {
    beforeEach(() => {
      vi.useFakeTimers();
    });

    afterEach(() => {
      vi.useRealTimers();
    });

    it("calculates duration from start time", () => {
      vi.setSystemTime(new Date(1000));
      const startTime = 500;
      const duration = calculateDuration(startTime);
      expect(duration).toBe(500);
    });

    it("returns undefined for undefined start time", () => {
      expect(calculateDuration(undefined)).toBeUndefined();
    });

    it("handles zero start time", () => {
      vi.setSystemTime(new Date(1000));
      const duration = calculateDuration(0);
      expect(duration).toBe(1000);
    });
  });

  describe("TOOL_NAME_MAP", () => {
    it("contains all expected tool mappings", () => {
      const expectedTools = [
        "read",
        "write",
        "edit",
        "glob",
        "grep",
        "bash",
        "webfetch",
        "todowrite",
        "todoread",
        "task",
        "codesearch",
        "websearch",
      ];

      for (const tool of expectedTools) {
        expect(TOOL_NAME_MAP).toHaveProperty(tool);
      }
    });

    it("all mapped values are capitalized", () => {
      for (const [key, value] of Object.entries(TOOL_NAME_MAP)) {
        expect(value[0]).toBe(value[0].toUpperCase());
      }
    });
  });
});
