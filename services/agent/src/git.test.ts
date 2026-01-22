import { describe, it, expect } from "vitest";
import { parseGitStatus, type GitFileChange } from "./git.js";

describe("parseGitStatus", () => {
  it("parses unstaged modified files", () => {
    const output = " M src/index.ts\n";
    const result = parseGitStatus(output);
    expect(result).toEqual([
      { path: "src/index.ts", status: "modified", staged: false },
    ]);
  });

  it("parses staged modified files", () => {
    const output = "M  src/index.ts\n";
    const result = parseGitStatus(output);
    expect(result).toEqual([
      { path: "src/index.ts", status: "modified", staged: true },
    ]);
  });

  it("parses staged added files", () => {
    const output = "A  src/new-file.ts\n";
    const result = parseGitStatus(output);
    expect(result).toEqual([
      { path: "src/new-file.ts", status: "added", staged: true },
    ]);
  });

  it("parses staged deleted files", () => {
    const output = "D  src/old-file.ts\n";
    const result = parseGitStatus(output);
    expect(result).toEqual([
      { path: "src/old-file.ts", status: "deleted", staged: true },
    ]);
  });

  it("parses unstaged deleted files", () => {
    const output = " D src/old-file.ts\n";
    const result = parseGitStatus(output);
    expect(result).toEqual([
      { path: "src/old-file.ts", status: "deleted", staged: false },
    ]);
  });

  it("parses untracked files", () => {
    const output = "?? src/untracked.ts\n";
    const result = parseGitStatus(output);
    expect(result).toEqual([
      { path: "src/untracked.ts", status: "untracked", staged: false },
    ]);
  });

  it("parses renamed files", () => {
    const output = "R  old-name.ts -> new-name.ts\n";
    const result = parseGitStatus(output);
    expect(result).toEqual([
      { path: "new-name.ts", status: "renamed", staged: true },
    ]);
  });

  it("parses copied files", () => {
    const output = "C  original.ts -> copy.ts\n";
    const result = parseGitStatus(output);
    expect(result).toEqual([
      { path: "copy.ts", status: "copied", staged: true },
    ]);
  });

  it("parses ignored files", () => {
    const output = "!! node_modules/\n";
    const result = parseGitStatus(output);
    expect(result).toEqual([
      { path: "node_modules/", status: "ignored", staged: false },
    ]);
  });

  it("parses unmerged files (both modified)", () => {
    const output = "UU src/conflict.ts\n";
    const result = parseGitStatus(output);
    expect(result).toEqual([
      { path: "src/conflict.ts", status: "unmerged", staged: false },
    ]);
  });

  it("parses multiple files", () => {
    const output = `M  staged.ts
 M unstaged.ts
A  added.ts
?? untracked.ts
`;
    const result = parseGitStatus(output);
    expect(result).toHaveLength(4);
    expect(result[0]).toEqual({ path: "staged.ts", status: "modified", staged: true });
    expect(result[1]).toEqual({ path: "unstaged.ts", status: "modified", staged: false });
    expect(result[2]).toEqual({ path: "added.ts", status: "added", staged: true });
    expect(result[3]).toEqual({ path: "untracked.ts", status: "untracked", staged: false });
  });

  it("handles empty output", () => {
    const result = parseGitStatus("");
    expect(result).toEqual([]);
  });

  it("handles paths with spaces", () => {
    const output = " M src/file with spaces.ts\n";
    const result = parseGitStatus(output);
    expect(result).toEqual([
      { path: "src/file with spaces.ts", status: "modified", staged: false },
    ]);
  });

  it("handles both staged and unstaged changes on same file", () => {
    // When a file has both staged and unstaged modifications, git shows MM
    const output = "MM src/both.ts\n";
    const result = parseGitStatus(output);
    // The current implementation prioritizes worktree status (M in position 1)
    // so this shows as unstaged modified
    expect(result).toEqual([
      { path: "src/both.ts", status: "modified", staged: false },
    ]);
  });
});
