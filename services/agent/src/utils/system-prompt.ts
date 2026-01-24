/**
 * System prompt builder - constructs the system prompt for Claude
 */

const WORKSPACE_DIR = "/agent/workspace";

export interface SystemPromptConfig {
  currentGitRepo: string | null;
}

/**
 * Build the system prompt for Claude Code SDK
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
    "- It is safe to run any commands - the sandbox is isolated",
    "",
    "## Tools",
    "",
    "- **Node.js 24** is pre-installed and available via `node`, `npm`, `npx`",
    "- **mise** is installed for managing additional tool versions (Python, Go, Rust, etc.)",
    "  - Install: `mise install go@latest && mise use -g go@latest` (may take 1-2 min)",
    "  - After install, tools are in PATH (e.g., `go version`)",
    "  - Check installed: `mise list`",
    "  - Common tools: python, go, rust, java, ruby",
  ];

  if (config.currentGitRepo) {
    lines.push(
      "",
      "## Repository",
      "",
      `The repository ${config.currentGitRepo} has been cloned to ${WORKSPACE_DIR}.`
    );
  }

  return {
    type: "preset",
    preset: "claude_code",
    append: lines.join("\n"),
  };
}
