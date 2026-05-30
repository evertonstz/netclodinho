/**
 * zmx session lifecycle management.
 *
 * Manages zmx child processes and Unix socket connections.
 * Each zmx session is a daemon process with its own PTY.
 * Sessions survive agent restarts (daemon keeps running independently).
 *
 * For future multi-tab support, session names follow the pattern:
 *   netclode.<sessionId>.<tabId>
 * Currently tabId defaults to "0".
 */

import { spawn, type ChildProcess, exec } from "node:child_process";
import { existsSync, mkdirSync } from "node:fs";
import { ZmxSocket, Tag } from "./zmx-socket.js";

/** Session naming prefix to avoid collisions with non-Netclode zmx sessions */
const SESSION_PREFIX = "netclode.";

/** Directory for zmx Unix sockets */
const ZMX_DIR = process.env.ZMX_DIR || "/tmp/netclode-zmx";

function sessionName(sessionId: string, tabId: string = "0"): string {
  return `${SESSION_PREFIX}${sessionId}.${tabId}`;
}

function socketPath(sessionId: string, tabId: string = "0"): string {
  return `${ZMX_DIR}/${sessionName(sessionId, tabId)}`;
}

export class ZmxService {
  private sockets = new Map<string, ZmxSocket>();
  private processes = new Map<string, ChildProcess>();

  constructor() {
    mkdirSync(ZMX_DIR, { recursive: true });
  }

  /**
   * Ensure a zmx session exists, returning a connected ZmxSocket.
   * Reuses existing daemon if socket already present, spawns new one otherwise.
   */
  async ensureSession(sessionId: string, tabId: string = "0"): Promise<ZmxSocket> {
    const name = sessionName(sessionId, tabId);
    const path = socketPath(sessionId, tabId);

    // Return existing socket if still connected
    const existing = this.sockets.get(name);
    if (existing) return existing;

    // Check if daemon socket already exists on disk
    if (existsSync(path)) {
      try {
        const sock = new ZmxSocket(path);
        this.sockets.set(name, sock);
        return sock;
      } catch {
        // Socket exists but is stale — remove and recreate
      }
    }

    // Spawn new zmx daemon
    console.log(`[zmx] Spawning session: ${name}`);
    const proc = spawn("zmx", ["attach", name], {
      env: { ...process.env, ZMX_DIR },
      stdio: "ignore",
      detached: true,
    });
    proc.unref();
    this.processes.set(name, proc);

    // Wait for socket to appear
    await this.waitForSocket(path, 5000);

    const sock = new ZmxSocket(path);
    this.sockets.set(name, sock);
    return sock;
  }

  /**
   * Send initial resize on first connect
   */
  async initSession(sessionId: string, cols: number, rows: number, tabId: string = "0"): Promise<ZmxSocket> {
    const sock = await this.ensureSession(sessionId, tabId);
    sock.writeResize({ cols, rows });
    return sock;
  }

  /**
   * Get terminal history for session restoration.
   * Executes `zmx history <name> --vt` which produces a VT-encoded stream
   * that, when fed to any terminal emulator, reproduces the exact visual state.
   */
  async getHistory(sessionId: string, tabId: string = "0"): Promise<string> {
    const name = sessionName(sessionId, tabId);
    return new Promise((resolve, reject) => {
      exec(`zmx history ${name} --vt`, {
        env: { ZMX_DIR },
        timeout: 10000,
      }, (err, stdout) => {
        if (err) {
          console.log(`[zmx] History unavailable for ${name}: ${err.message}`);
          resolve(""); // Return empty — no history to replay
        } else {
          resolve(stdout);
        }
      });
    });
  }

  /**
   * Kill a zmx session daemon
   */
  killSession(sessionId: string, tabId: string = "0"): void {
    const name = sessionName(sessionId, tabId);
    const sock = this.sockets.get(name);
    if (sock) {
      sock.writeFrame(Tag.Kill);
      sock.close();
      this.sockets.delete(name);
    }
    this.processes.delete(name);
  }

  /**
   * Detach all clients from a session (keep daemon running)
   */
  detachSession(sessionId: string, tabId: string = "0"): void {
    const name = sessionName(sessionId, tabId);
    const sock = this.sockets.get(name);
    if (sock) {
      sock.writeFrame(Tag.DetachAll);
      sock.close();
      this.sockets.delete(name);
    }
  }

  /**
   * Clean up all sessions
   */
  shutdown(): void {
    for (const [name, sock] of this.sockets) {
      sock.writeFrame(Tag.Detach);
      sock.close();
    }
    this.sockets.clear();
    this.processes.clear();
  }

  private waitForSocket(path: string, timeoutMs: number): Promise<void> {
    const deadline = Date.now() + timeoutMs;
    return new Promise((resolve, reject) => {
      const check = () => {
        if (existsSync(path)) {
          resolve();
        } else if (Date.now() > deadline) {
          reject(new Error(`zmx socket did not appear: ${path}`));
        } else {
          setTimeout(check, 100);
        }
      };
      check();
    });
  }
}

/** Singleton service instance */
let zmxService: ZmxService | null = null;

export function getZmxService(): ZmxService {
  if (!zmxService) {
    zmxService = new ZmxService();
  }
  return zmxService;
}
