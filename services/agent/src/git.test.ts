import { describe, it, expect } from "vitest";
import { parseGitStatus, repoDirName, getRepoPath, getRepoPrefix, type GitFileChange } from "./git.js";

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

describe("repoDirName", () => {
  it("derives a stable directory name from a GitHub URL", () => {
    expect(repoDirName("https://github.com/owner/repo.git")).toBe("owner__repo");
  });

  it("derives a stable directory name from owner/repo", () => {
    expect(repoDirName("owner/repo")).toBe("owner__repo");
  });
});

describe("getRepoPath", () => {
  const WORKSPACE_DIR = "/agent/workspace";

  it("returns workspace dir directly for single repo", () => {
    expect(getRepoPath("https://github.com/owner/repo.git", 1, WORKSPACE_DIR)).toBe(WORKSPACE_DIR);
  });

  it("returns workspace dir directly for single repo regardless of repo URL", () => {
    expect(getRepoPath("owner/repo", 1, WORKSPACE_DIR)).toBe(WORKSPACE_DIR);
  });

  it("returns subdirectory for multiple repos", () => {
    expect(getRepoPath("https://github.com/owner/repo.git", 2, WORKSPACE_DIR)).toBe(`${WORKSPACE_DIR}/owner__repo`);
  });

  it("returns unique subdirectories for each repo in multi-repo setup", () => {
    const repo1Path = getRepoPath("https://github.com/owner/repo1.git", 2, WORKSPACE_DIR);
    const repo2Path = getRepoPath("https://github.com/owner/repo2.git", 2, WORKSPACE_DIR);
    expect(repo1Path).toBe(`${WORKSPACE_DIR}/owner__repo1`);
    expect(repo2Path).toBe(`${WORKSPACE_DIR}/owner__repo2`);
    expect(repo1Path).not.toBe(repo2Path);
  });
});

describe("getRepoPrefix", () => {
  it("returns empty string for single repo", () => {
    expect(getRepoPrefix("https://github.com/owner/repo.git", 1)).toBe("");
  });

  it("returns repo dir name for multiple repos", () => {
    expect(getRepoPrefix("https://github.com/owner/repo.git", 2)).toBe("owner__repo");
  });

  it("returns unique prefixes for each repo in multi-repo setup", () => {
    const prefix1 = getRepoPrefix("https://github.com/owner/repo1.git", 2);
    const prefix2 = getRepoPrefix("https://github.com/owner/repo2.git", 2);
    expect(prefix1).toBe("owner__repo1");
    expect(prefix2).toBe("owner__repo2");
    expect(prefix1).not.toBe(prefix2);
  });
});
