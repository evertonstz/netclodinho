/**
 * Snapshot service - manages JuiceFS workspace snapshots
 *
 * JuiceFS clone is a metadata-only copy-on-write operation.
 * Very fast regardless of workspace size.
 */

import { execSync } from "child_process";
import { existsSync, mkdirSync, readdirSync, statSync, rmSync, writeFileSync, readFileSync } from "fs";
import { join } from "path";

const WORKSPACE_DIR = "/agent/workspace";
const SNAPSHOTS_DIR = "/agent/.snapshots";

interface SnapshotMetadata {
  id: string;
  name: string;
  createdAt: string;
}

interface SnapshotInfo {
  id: string;
  name: string;
  createdAt: Date;
  sizeBytes: number;
}

/**
 * Create a snapshot of the current workspace using JuiceFS clone
 */
export async function createSnapshot(
  snapshotId: string,
  name: string
): Promise<{ success: boolean; error?: string; sizeBytes?: number }> {
  try {
    // Ensure snapshots directory exists
    if (!existsSync(SNAPSHOTS_DIR)) {
      mkdirSync(SNAPSHOTS_DIR, { recursive: true });
    }

    const snapshotPath = join(SNAPSHOTS_DIR, snapshotId);

    // Check if snapshot already exists
    if (existsSync(snapshotPath)) {
      return { success: false, error: `Snapshot ${snapshotId} already exists` };
    }

    // Check if workspace exists
    if (!existsSync(WORKSPACE_DIR)) {
      return { success: false, error: "Workspace directory does not exist" };
    }

    console.log(`[snapshot] Creating snapshot ${snapshotId}: "${name}"`);

    // Use JuiceFS clone for CoW snapshot
    // This is a metadata-only operation - very fast!
    // Falls back to cp -a if juicefs is not available (for local dev)
    try {
      execSync(`juicefs clone "${WORKSPACE_DIR}" "${snapshotPath}"`, {
        timeout: 60000, // 60s timeout (should be instant for JuiceFS)
        stdio: "pipe",
      });
    } catch (juicefsError) {
      // Fallback to regular copy for non-JuiceFS environments (local dev)
      console.log("[snapshot] JuiceFS clone not available, falling back to cp -a");
      execSync(`cp -a "${WORKSPACE_DIR}" "${snapshotPath}"`, {
        timeout: 300000, // 5min timeout for regular copy
        stdio: "pipe",
      });
    }

    // Write metadata file
    const metadata: SnapshotMetadata = {
      id: snapshotId,
      name: name,
      createdAt: new Date().toISOString(),
    };
    const metadataPath = join(SNAPSHOTS_DIR, `${snapshotId}.meta.json`);
    writeFileSync(metadataPath, JSON.stringify(metadata, null, 2));

    // Get logical size
    let sizeBytes = 0;
    try {
      const sizeOutput = execSync(`du -sb "${snapshotPath}" 2>/dev/null | cut -f1`, {
        encoding: "utf-8",
        stdio: ["pipe", "pipe", "pipe"],
      }).trim();
      sizeBytes = parseInt(sizeOutput, 10) || 0;
    } catch {
      // Ignore size errors
    }

    console.log(`[snapshot] Created snapshot ${snapshotId} (${sizeBytes} bytes)`);
    return { success: true, sizeBytes };
  } catch (err) {
    const error = err instanceof Error ? err.message : String(err);
    console.error(`[snapshot] Failed to create snapshot:`, error);
    return { success: false, error };
  }
}

/**
 * Restore workspace from a snapshot
 *
 * Strategy:
 * 1. Move current workspace to .workspace-backup
 * 2. Clone snapshot to workspace
 * 3. Delete backup on success
 */
export async function restoreSnapshot(
  snapshotId: string
): Promise<{ success: boolean; error?: string }> {
  const snapshotPath = join(SNAPSHOTS_DIR, snapshotId);
  const backupPath = "/agent/.workspace-backup";

  try {
    // Verify snapshot exists
    if (!existsSync(snapshotPath)) {
      return { success: false, error: `Snapshot ${snapshotId} not found` };
    }

    console.log(`[snapshot] Restoring workspace from snapshot ${snapshotId}`);

    // Move current workspace to backup
    if (existsSync(backupPath)) {
      rmSync(backupPath, { recursive: true, force: true });
    }

    if (existsSync(WORKSPACE_DIR)) {
      execSync(`mv "${WORKSPACE_DIR}" "${backupPath}"`, { stdio: "pipe" });
    }

    // Clone snapshot to workspace
    try {
      execSync(`juicefs clone "${snapshotPath}" "${WORKSPACE_DIR}"`, {
        timeout: 60000,
        stdio: "pipe",
      });
    } catch (juicefsError) {
      // Fallback to regular copy for non-JuiceFS environments
      console.log("[snapshot] JuiceFS clone not available, falling back to cp -a");
      execSync(`cp -a "${snapshotPath}" "${WORKSPACE_DIR}"`, {
        timeout: 300000,
        stdio: "pipe",
      });
    }

    // Success - remove backup
    if (existsSync(backupPath)) {
      rmSync(backupPath, { recursive: true, force: true });
    }

    console.log(`[snapshot] Restored workspace from snapshot ${snapshotId}`);
    return { success: true };
  } catch (err) {
    // Restore backup if clone failed
    if (existsSync(backupPath) && !existsSync(WORKSPACE_DIR)) {
      try {
        execSync(`mv "${backupPath}" "${WORKSPACE_DIR}"`, { stdio: "pipe" });
        console.log("[snapshot] Restored backup after failed restore");
      } catch {
        console.error("[snapshot] Failed to restore backup!");
      }
    }

    const error = err instanceof Error ? err.message : String(err);
    console.error(`[snapshot] Failed to restore snapshot:`, error);
    return { success: false, error };
  }
}

/**
 * List all snapshots for the current workspace
 */
export function listSnapshots(): SnapshotInfo[] {
  if (!existsSync(SNAPSHOTS_DIR)) {
    return [];
  }

  const snapshots: SnapshotInfo[] = [];
  const entries = readdirSync(SNAPSHOTS_DIR);

  for (const entry of entries) {
    if (entry.endsWith(".meta.json")) {
      continue; // Skip metadata files
    }

    const snapshotPath = join(SNAPSHOTS_DIR, entry);
    const metadataPath = join(SNAPSHOTS_DIR, `${entry}.meta.json`);

    try {
      if (statSync(snapshotPath).isDirectory()) {
        let metadata: SnapshotMetadata = {
          id: entry,
          name: entry,
          createdAt: new Date().toISOString(),
        };

        try {
          if (existsSync(metadataPath)) {
            metadata = JSON.parse(readFileSync(metadataPath, "utf-8"));
          }
        } catch {
          // Use defaults
        }

        // Get size
        let sizeBytes = 0;
        try {
          const sizeOutput = execSync(`du -sb "${snapshotPath}" 2>/dev/null | cut -f1`, {
            encoding: "utf-8",
            stdio: ["pipe", "pipe", "pipe"],
          }).trim();
          sizeBytes = parseInt(sizeOutput, 10) || 0;
        } catch {
          // Ignore size errors
        }

        snapshots.push({
          id: entry,
          name: metadata.name,
          createdAt: new Date(metadata.createdAt),
          sizeBytes,
        });
      }
    } catch {
      // Skip invalid entries
    }
  }

  // Sort by creation date, newest first
  return snapshots.sort((a, b) => b.createdAt.getTime() - a.createdAt.getTime());
}

/**
 * Delete a snapshot
 */
export function deleteSnapshot(snapshotId: string): { success: boolean; error?: string } {
  const snapshotPath = join(SNAPSHOTS_DIR, snapshotId);
  const metadataPath = join(SNAPSHOTS_DIR, `${snapshotId}.meta.json`);

  try {
    if (!existsSync(snapshotPath)) {
      return { success: false, error: `Snapshot ${snapshotId} not found` };
    }

    rmSync(snapshotPath, { recursive: true, force: true });

    if (existsSync(metadataPath)) {
      rmSync(metadataPath, { force: true });
    }

    console.log(`[snapshot] Deleted snapshot ${snapshotId}`);
    return { success: true };
  } catch (err) {
    const error = err instanceof Error ? err.message : String(err);
    console.error(`[snapshot] Failed to delete snapshot:`, error);
    return { success: false, error };
  }
}
