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
    "- **mise** is installed for managing tool versions (Node, Python, Go, Rust, etc.)",
    "  - Use `mise use node@22` to install and activate Node.js 22",
    "  - Use `mise use python@3.12` for Python",
    "  - Use `mise use go@latest` for Go",
    "  - See `mise --help` for more options",
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
