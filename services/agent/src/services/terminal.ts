/**
 * Terminal PTY management - handles pseudo-terminal creation and I/O
 */

import type { IPty } from "node-pty";
import * as pty from "node-pty";

const WORKSPACE_DIR = "/agent/workspace";

// Singleton PTY instance
let terminalPty: IPty | null = null;

// Use a Set of callbacks to support multiple concurrent terminal streams
const terminalOutputCallbacks = new Set<(data: string) => void>();

/**
 * Ensure a PTY exists, creating one if needed
 */
export function ensureTerminalPty(cols: number = 80, rows: number = 24): IPty {
  if (!terminalPty) {
    console.log(`[terminal] Spawning PTY: shell=${process.env.SHELL || "/bin/bash"}, cols=${cols}, rows=${rows}`);
    terminalPty = pty.spawn(process.env.SHELL || "/bin/bash", [], {
      name: "xterm-256color",
      cwd: WORKSPACE_DIR,
      cols,
      rows,
      env: process.env as Record<string, string>,
    });

    terminalPty.onData((data: string) => {
      // Broadcast to all connected terminal streams
      for (const callback of terminalOutputCallbacks) {
        callback(data);
      }
    });

    terminalPty.onExit(({ exitCode, signal }) => {
      console.log(`[terminal] PTY exited: code=${exitCode}, signal=${signal}`);
      terminalPty = null;
    });
  }
  return terminalPty;
}

/**
 * Get the current PTY instance (may be null)
 */
export function getTerminalPty(): IPty | null {
  return terminalPty;
}

/**
 * Write data to the PTY
 */
export function writeToTerminal(data: string): void {
  const p = ensureTerminalPty();
  p.write(data);
}

/**
 * Resize the PTY
 */
export function resizeTerminal(cols: number, rows: number): void {
  if (terminalPty) {
    console.log(`[terminal] Resizing PTY: cols=${cols}, rows=${rows}`);
    terminalPty.resize(cols, rows);
  } else {
    ensureTerminalPty(cols, rows);
  }
}

/**
 * Register a callback for terminal output
 * Returns an unregister function
 */
export function registerOutputCallback(callback: (data: string) => void): () => void {
  terminalOutputCallbacks.add(callback);
  return () => {
    terminalOutputCallbacks.delete(callback);
  };
}

/**
 * Terminal input type - flexible to handle protobuf oneOf semantics
 */
export interface TerminalInputMessage {
  input:
    | { case: "data"; value: string }
    | { case: "resize"; value: { cols: number; rows: number } }
    | { case: undefined; value?: undefined };
}

/**
 * Create a terminal stream handler that processes input and yields output
 */
export async function* handleTerminalStream(
  requests: AsyncIterable<TerminalInputMessage>
): AsyncGenerator<string> {
  console.log("[terminal] Terminal stream started");

  // Set up output callback for this stream
  const outputQueue: string[] = [];
  let resolveWait: (() => void) | null = null;

  const outputCallback = (data: string) => {
    outputQueue.push(data);
    if (resolveWait) {
      resolveWait();
      resolveWait = null;
    }
  };

  // Register this stream's callback
  const unregister = registerOutputCallback(outputCallback);

  // Track if stream is still active
  let streamActive = true;

  // Process input in background - handle errors to avoid unhandled rejections
  const inputProcessor = (async () => {
    try {
      for await (const input of requests) {
        if (!streamActive) break;
        if (input.input.case === "data") {
          writeToTerminal(input.input.value);
        } else if (input.input.case === "resize") {
          const resize = input.input.value;
          resizeTerminal(resize.cols, resize.rows);
        }
        // Ignore undefined case
      }
    } catch (err) {
      // Stream closed or error - this is expected when client disconnects
      if (streamActive) {
        console.log("[terminal] Input stream error:", err);
      }
    }
  })();

  // Yield outputs
  try {
    while (streamActive) {
      if (outputQueue.length > 0) {
        const data = outputQueue.shift()!;
        yield data;
      } else {
        // Wait for more output
        await new Promise<void>((resolve) => {
          resolveWait = resolve;
          // Timeout to allow checking if stream should close
          setTimeout(resolve, 100);
        });
      }
    }
  } finally {
    // Signal that stream is ending
    streamActive = false;

    // Unregister this stream's callback immediately to stop receiving output
    unregister();

    // Don't await inputProcessor - it will complete naturally when the requests
    // stream closes, or exit early via the streamActive check. Awaiting here would
    // block if the client disconnected before closing the request stream.
    // The catch block in inputProcessor handles any errors.

    console.log("[terminal] Terminal stream ended");
  }
}
