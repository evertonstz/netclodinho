import { spawn } from "child_process";
import { existsSync, writeFileSync, mkdirSync } from "fs";
import { dirname } from "path";

const CONTROL_PLANE_URL = process.env.CONTROL_PLANE_URL || "http://control-plane.netclode.svc.cluster.local";

interface CloneEvent {
  kind: "repo_clone";
  timestamp: string;
  repo: string;
  stage: "starting" | "cloning" | "done" | "error";
  message: string;
}

/**
 * Report a clone event to the control plane for broadcasting to clients.
 */
async function reportEvent(sessionId: string, event: CloneEvent): Promise<void> {
  if (!sessionId) {
    console.log("[git] No SESSION_ID, skipping event report");
    return;
  }

  try {
    const response = await fetch(`${CONTROL_PLANE_URL}/internal/session/${sessionId}/event`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(event),
    });
    if (!response.ok) {
      console.warn(`[git] Failed to report event: ${response.status}`);
    }
  } catch (error) {
    // Fire and forget - don't fail if control plane is unreachable
    console.warn("[git] Failed to report event:", error);
  }
}

/**
 * Run a git command and return the output.
 */
export function runGit(args: string[], cwd?: string): Promise<{ code: number; stdout: string; stderr: string }> {
  return new Promise((resolve) => {
    const proc = spawn("git", args, {
      cwd,
      env: process.env,
    });

    let stdout = "";
    let stderr = "";

    proc.stdout.on("data", (data) => {
      stdout += data.toString();
    });

    proc.stderr.on("data", (data) => {
      stderr += data.toString();
      // Log progress output (git clone --progress writes to stderr)
      process.stderr.write(data);
    });

    proc.on("close", (code) => {
      resolve({ code: code ?? 1, stdout, stderr });
    });
  });
}

/**
 * Configure git credentials for GitHub token authentication.
 */
async function configureGitCredentials(token: string): Promise<void> {
  const credentialsFile = "/agent/.git-credentials";
  const configDir = dirname(credentialsFile);

  if (!existsSync(configDir)) {
    mkdirSync(configDir, { recursive: true });
  }

  writeFileSync(credentialsFile, `https://x-access-token:${token}@github.com\n`, { mode: 0o600 });

  await runGit(["config", "--global", "credential.helper", `store --file=${credentialsFile}`]);
  await runGit(["config", "--global", "user.name", "Netclode Agent"]);
  await runGit(["config", "--global", "user.email", "agent@netclode.local"]);
}

/**
 * Clone or update a repository.
 */
/**
 * Parsed git file change from status --porcelain
 */
export interface GitFileChange {
  path: string;
  status: "modified" | "added" | "deleted" | "renamed" | "copied" | "untracked" | "ignored" | "unmerged";
  staged: boolean;
}

/**
 * Parse git status --porcelain=v1 output into structured data
 */
export function parseGitStatus(output: string): GitFileChange[] {
  const files: GitFileChange[] = [];
  const lines = output.split("\n").filter((line) => line.length > 0);

  for (const line of lines) {
    const indexStatus = line[0];
    const workTreeStatus = line[1];
    const path = line.substring(3).split(" -> ").pop() || line.substring(3);

    let status: GitFileChange["status"];
    let staged = false;

    if (workTreeStatus === "M") {
      status = "modified";
    } else if (workTreeStatus === "D") {
      status = "deleted";
    } else if (workTreeStatus === "?") {
      status = "untracked";
    } else if (workTreeStatus === "!") {
      status = "ignored";
    } else if (workTreeStatus === "U" || indexStatus === "U") {
      status = "unmerged";
    } else if (indexStatus === "M") {
      status = "modified";
      staged = true;
    } else if (indexStatus === "A") {
      status = "added";
      staged = true;
    } else if (indexStatus === "D") {
      status = "deleted";
      staged = true;
    } else if (indexStatus === "R") {
      status = "renamed";
      staged = true;
    } else if (indexStatus === "C") {
      status = "copied";
      staged = true;
    } else {
      status = "modified";
    }

    files.push({ path, status, staged });
  }

  return files;
}

/**
 * Get the git status for a workspace
 */
export async function getGitStatus(workspaceDir: string): Promise<GitFileChange[]> {
  console.log("[git] Getting git status");
  const result = await runGit(["status", "--porcelain=v1"], workspaceDir);
  const files = parseGitStatus(result.stdout);
  console.log(`[git] Git status: ${files.length} changed files`);
  return files;
}

/**
 * Get the git diff for a workspace or specific file
 */
export async function getGitDiff(workspaceDir: string, file?: string): Promise<string> {
  console.log(`[git] Getting git diff for: ${file || "all files"}`);
  const args = ["diff", "--no-color"];
  if (file) {
    args.push("--", file);
  }
  const result = await runGit(args, workspaceDir);
  console.log(`[git] Git diff: ${result.stdout.length} chars`);
  return result.stdout;
}

export async function setupRepository(
  repoUrl: string,
  workspaceDir: string,
  sessionId: string,
  githubToken?: string
): Promise<void> {
  console.log(`[git] Setting up repository: ${repoUrl}`);

  // Configure credentials if token provided
  if (githubToken) {
    console.log("[git] Configuring git credentials...");
    await configureGitCredentials(githubToken);
  }

  const gitDir = `${workspaceDir}/.git`;
  const isExistingRepo = existsSync(gitDir);

  if (isExistingRepo) {
    // Repository already exists - pull latest changes
    await reportEvent(sessionId, {
      kind: "repo_clone",
      timestamp: new Date().toISOString(),
      repo: repoUrl,
      stage: "starting",
      message: "Pulling latest changes...",
    });

    console.log("[git] Repository already exists, pulling latest changes...");
    await runGit(["config", "--add", "safe.directory", workspaceDir], workspaceDir);

    const result = await runGit(["pull", "--ff-only"], workspaceDir);

    if (result.code === 0) {
      await reportEvent(sessionId, {
        kind: "repo_clone",
        timestamp: new Date().toISOString(),
        repo: repoUrl,
        stage: "done",
        message: "Repository updated",
      });
      console.log("[git] Pull completed successfully");
    } else {
      await reportEvent(sessionId, {
        kind: "repo_clone",
        timestamp: new Date().toISOString(),
        repo: repoUrl,
        stage: "done",
        message: "Pull failed, using existing state",
      });
      console.warn("[git] Pull failed, continuing with existing state");
    }
  } else {
    // Fresh clone
    await reportEvent(sessionId, {
      kind: "repo_clone",
      timestamp: new Date().toISOString(),
      repo: repoUrl,
      stage: "starting",
      message: "Cloning repository...",
    });

    console.log("[git] Cloning repository...");
    const result = await runGit(["clone", "--progress", repoUrl, workspaceDir]);

    if (result.code === 0) {
      await runGit(["config", "--add", "safe.directory", workspaceDir], workspaceDir);

      await reportEvent(sessionId, {
        kind: "repo_clone",
        timestamp: new Date().toISOString(),
        repo: repoUrl,
        stage: "done",
        message: "Repository cloned successfully",
      });
      console.log("[git] Clone completed successfully");
    } else {
      await reportEvent(sessionId, {
        kind: "repo_clone",
        timestamp: new Date().toISOString(),
        repo: repoUrl,
        stage: "error",
        message: `Failed to clone: ${result.stderr.slice(0, 200)}`,
      });
      console.error("[git] Clone failed:", result.stderr);
      // Don't throw - agent can still work without the repo
    }
  }
}
