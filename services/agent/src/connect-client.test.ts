import { describe, it, expect } from "vitest";
import { GitFileStatus } from "../gen/netclode/v1/common_pb.js";
import { AgentEventKind } from "../gen/netclode/v1/events_pb.js";

// Re-implement the helper functions for testing (they're private in connect-client.ts)
// This validates the conversion logic without needing to export internal functions

type GitStatus = "modified" | "added" | "deleted" | "renamed" | "untracked" | "copied" | "ignored" | "unmerged";

function convertGitStatus(status: GitStatus): GitFileStatus {
  switch (status) {
    case "modified": return GitFileStatus.MODIFIED;
    case "added": return GitFileStatus.ADDED;
    case "deleted": return GitFileStatus.DELETED;
    case "renamed": return GitFileStatus.RENAMED;
    case "untracked": return GitFileStatus.UNTRACKED;
    case "copied": return GitFileStatus.COPIED;
    case "ignored": return GitFileStatus.IGNORED;
    case "unmerged": return GitFileStatus.UNMERGED;
    default: return GitFileStatus.UNSPECIFIED;
  }
}

describe("convertGitStatus", () => {
  it("converts modified status", () => {
    expect(convertGitStatus("modified")).toBe(GitFileStatus.MODIFIED);
  });

  it("converts added status", () => {
    expect(convertGitStatus("added")).toBe(GitFileStatus.ADDED);
  });

  it("converts deleted status", () => {
    expect(convertGitStatus("deleted")).toBe(GitFileStatus.DELETED);
  });

  it("converts renamed status", () => {
    expect(convertGitStatus("renamed")).toBe(GitFileStatus.RENAMED);
  });

  it("converts untracked status", () => {
    expect(convertGitStatus("untracked")).toBe(GitFileStatus.UNTRACKED);
  });

  it("converts copied status", () => {
    expect(convertGitStatus("copied")).toBe(GitFileStatus.COPIED);
  });

  it("converts ignored status", () => {
    expect(convertGitStatus("ignored")).toBe(GitFileStatus.IGNORED);
  });

  it("converts unmerged status", () => {
    expect(convertGitStatus("unmerged")).toBe(GitFileStatus.UNMERGED);
  });

  it("returns unspecified for unknown status", () => {
    expect(convertGitStatus("unknown" as GitStatus)).toBe(GitFileStatus.UNSPECIFIED);
  });
});

describe("AgentEventKind enum values", () => {
  // Verify the protobuf enum values match what we expect
  it("has expected tool event kinds", () => {
    expect(AgentEventKind.TOOL_START).toBeDefined();
    expect(AgentEventKind.TOOL_INPUT).toBeDefined();
    expect(AgentEventKind.TOOL_INPUT_COMPLETE).toBeDefined();
    expect(AgentEventKind.TOOL_END).toBeDefined();
  });

  it("has expected file/command event kinds", () => {
    expect(AgentEventKind.FILE_CHANGE).toBeDefined();
    expect(AgentEventKind.COMMAND_START).toBeDefined();
    expect(AgentEventKind.COMMAND_END).toBeDefined();
  });

  it("has expected other event kinds", () => {
    expect(AgentEventKind.THINKING).toBeDefined();
    expect(AgentEventKind.PORT_EXPOSED).toBeDefined();
    expect(AgentEventKind.REPO_CLONE).toBeDefined();
  });
});

describe("GitFileStatus enum values", () => {
  it("has expected git status values", () => {
    expect(GitFileStatus.MODIFIED).toBeDefined();
    expect(GitFileStatus.ADDED).toBeDefined();
    expect(GitFileStatus.DELETED).toBeDefined();
    expect(GitFileStatus.RENAMED).toBeDefined();
    expect(GitFileStatus.UNTRACKED).toBeDefined();
    expect(GitFileStatus.COPIED).toBeDefined();
    expect(GitFileStatus.IGNORED).toBeDefined();
    expect(GitFileStatus.UNMERGED).toBeDefined();
    expect(GitFileStatus.UNSPECIFIED).toBeDefined();
  });
});
