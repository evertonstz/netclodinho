import { describe, expect, it } from "vitest";
import { ClaudeSDKAdapter } from "./claude/index.js";
import { OpenCodeAdapter } from "./opencode/index.js";
import { CopilotAdapter } from "./copilot/index.js";
import { CodexAdapter } from "./codex/index.js";

describe("backend capability declarations", () => {
  it("declares Claude backend capabilities", () => {
    expect(new ClaudeSDKAdapter().capabilities).toEqual(expect.objectContaining({
      interrupt: true,
      toolStreaming: true,
      thinkingStreaming: true,
    }));
  });

  it("declares OpenCode backend capabilities", () => {
    expect(new OpenCodeAdapter().capabilities).toEqual(expect.objectContaining({
      interrupt: true,
      toolStreaming: true,
      thinkingStreaming: true,
    }));
  });

  it("declares Copilot backend capabilities", () => {
    expect(new CopilotAdapter().capabilities).toEqual(expect.objectContaining({
      interrupt: true,
      toolStreaming: true,
      thinkingStreaming: false,
    }));
  });

  it("declares Codex backend capabilities", () => {
    expect(new CodexAdapter().capabilities).toEqual(expect.objectContaining({
      interrupt: true,
      toolStreaming: true,
      thinkingStreaming: false,
    }));
  });
});
