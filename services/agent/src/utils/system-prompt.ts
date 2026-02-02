/**
 * System prompt builder - constructs the system prompt for Claude
 */

import { getRepoPath } from "../git.js";
import { WORKSPACE_DIR } from "../constants.js";

export interface SystemPromptConfig {
  currentGitRepos: string[];
}

/**
 * Build the system prompt for Claude Agent SDK
 */
export function buildSystemPrompt(config: SystemPromptConfig): {
  type: "preset";
  preset: "claude_code";
  append: string;
} {
  const lines = [
    "## Environment",
    "",
    "You are running inside an isolated sandbox (Kata Container microVM).",
    `- Working directory: ${WORKSPACE_DIR}`,
    "- Everything persists across sessions: files, Docker images, installed tools, caches",
    "- You have full shell, network, and Docker access",
    "- You have sudo access (passwordless) for system administration tasks",
    "- It is safe to run any commands - the sandbox is isolated",
    "",
    "## Tools",
    "",
    "- **Node.js 24** is pre-installed and available via `node`, `npm`, `npx`",
    "- **gh** (GitHub CLI) is pre-installed for GitHub operations (issues, PRs, repos, etc.)",
    "  - Authenticated automatically when working with a repository",
    "  - Examples: `gh pr list`, `gh issue create`, `gh repo view`",
    "- **mise** is installed for managing additional tool versions (Python, Go, Rust, etc.)",
    "  - Install: `mise install go@latest && mise use -g go@latest` (may take 1-2 min)",
    "  - After install, tools are in PATH (e.g., `go version`)",
    "  - Check installed: `mise list`",
    "  - Common tools: python, go, rust, java, ruby",
  ];

  if (config.currentGitRepos.length > 0) {
    const totalRepos = config.currentGitRepos.length;
    lines.push("", "## Repositories", "", "Repositories cloned under /agent/workspace:");
    config.currentGitRepos.forEach((repo, index) => {
      const repoPath = getRepoPath(repo, totalRepos, WORKSPACE_DIR);
      const primary = index == 0 ? " (primary)" : "";
      lines.push(`- ${repo}${primary} -> ${repoPath}`);
    });
  }

  return {
    type: "preset",
    preset: "claude_code",
    append: lines.join("\n"),
  };
}
