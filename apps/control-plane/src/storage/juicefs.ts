/**
 * JuiceFS storage operations
 *
 * Manages session workspaces on JuiceFS
 */
import { $ } from "bun";
import { config } from "../config";

const JUICEFS_ROOT = config.juicefsRoot;

/**
 * Create a workspace directory for a session
 */
export async function createWorkspace(sessionId: string): Promise<void> {
  const path = `${JUICEFS_ROOT}/sessions/${sessionId}/workspace`;
  await $`mkdir -p ${path}`;
  console.log(`Created workspace: ${path}`);
}

/**
 * Delete a session's workspace
 */
export async function deleteWorkspace(sessionId: string): Promise<void> {
  const path = `${JUICEFS_ROOT}/sessions/${sessionId}`;
  await $`rm -rf ${path}`;
  console.log(`Deleted workspace: ${path}`);
}

/**
 * Check if workspace exists
 */
export async function workspaceExists(sessionId: string): Promise<boolean> {
  const path = `${JUICEFS_ROOT}/sessions/${sessionId}/workspace`;
  try {
    await $`test -d ${path}`.quiet();
    return true;
  } catch {
    return false;
  }
}

/**
 * Clone a git repository into the workspace
 */
export async function cloneRepo(sessionId: string, repoUrl: string): Promise<void> {
  const path = `${JUICEFS_ROOT}/sessions/${sessionId}/workspace`;

  // Clear workspace first
  await $`rm -rf ${path}/*`.quiet();

  // Clone repo
  console.log(`Cloning ${repoUrl} to ${path}`);
  await $`git clone --depth 1 ${repoUrl} ${path}`;
}

/**
 * Create a snapshot of a workspace
 */
export async function createSnapshot(sessionId: string, name: string): Promise<void> {
  const workspacePath = `${JUICEFS_ROOT}/sessions/${sessionId}/workspace`;
  const snapshotDir = `${JUICEFS_ROOT}/sessions/${sessionId}/snapshots`;
  const snapshotPath = `${snapshotDir}/${name}`;

  // Create snapshots directory
  await $`mkdir -p ${snapshotDir}`;

  // Use JuiceFS clone for CoW snapshot (if available) or fallback to cp
  try {
    await $`juicefs clone ${workspacePath} ${snapshotPath}`;
  } catch {
    // Fallback to regular copy
    await $`cp -r ${workspacePath} ${snapshotPath}`;
  }

  console.log(`Created snapshot: ${snapshotPath}`);
}

/**
 * Restore a workspace from snapshot
 */
export async function restoreSnapshot(sessionId: string, name: string): Promise<void> {
  const workspacePath = `${JUICEFS_ROOT}/sessions/${sessionId}/workspace`;
  const snapshotPath = `${JUICEFS_ROOT}/sessions/${sessionId}/snapshots/${name}`;

  // Remove current workspace
  await $`rm -rf ${workspacePath}`;

  // Restore from snapshot
  try {
    await $`juicefs clone ${snapshotPath} ${workspacePath}`;
  } catch {
    // Fallback to regular copy
    await $`cp -r ${snapshotPath} ${workspacePath}`;
  }

  console.log(`Restored snapshot: ${name}`);
}

/**
 * List snapshots for a session
 */
export async function listSnapshots(sessionId: string): Promise<string[]> {
  const snapshotDir = `${JUICEFS_ROOT}/sessions/${sessionId}/snapshots`;

  try {
    const result = await $`ls ${snapshotDir}`.text();
    return result.trim().split("\n").filter(Boolean);
  } catch {
    return [];
  }
}

/**
 * Delete a snapshot
 */
export async function deleteSnapshot(sessionId: string, name: string): Promise<void> {
  const snapshotPath = `${JUICEFS_ROOT}/sessions/${sessionId}/snapshots/${name}`;
  await $`rm -rf ${snapshotPath}`;
  console.log(`Deleted snapshot: ${snapshotPath}`);
}

/**
 * Get workspace size in bytes
 */
export async function getWorkspaceSize(sessionId: string): Promise<number> {
  const path = `${JUICEFS_ROOT}/sessions/${sessionId}`;

  try {
    const result = await $`du -sb ${path}`.text();
    return parseInt(result.split("\t")[0], 10);
  } catch {
    return 0;
  }
}

/**
 * List all sessions with workspaces
 */
export async function listWorkspaces(): Promise<string[]> {
  const sessionsDir = `${JUICEFS_ROOT}/sessions`;

  try {
    const result = await $`ls ${sessionsDir}`.text();
    return result.trim().split("\n").filter(Boolean);
  } catch {
    return [];
  }
}
