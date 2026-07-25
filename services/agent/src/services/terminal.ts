/**
 * Terminal management via zmx (replaces node-pty).
 *
 * Each zmx session runs as an independent daemon with its own PTY,
 * surviving agent restarts. Multiple sessions (tabs) are supported
 * via sessionId + optional tabId.
 */

import { StringDecoder } from "node:string_decoder";
import { Tag, type ZmxSocket } from "./zmx-socket.js";
import { getZmxService } from "./zmx-service.js";

// Terminal output callbacks
// globalTerminalOutputCallback: set by connect-client for streaming to control plane
// terminalOutputCallbacks: set of callbacks for concurrent terminal stream consumers
let globalTerminalOutputCallback: ((data: string) => void) | null = null;
const terminalOutputCallbacks = new Set<(data: string) => void>();

// Active zmx sockets by session key: "sessionId.tabId"
const activeSockets = new Map<string, ZmxSocket>();

// In-flight socket creations, so concurrent input+resize before the socket
// exists share one connection instead of racing into two sockets with
// duplicate frame listeners (duplicated output for every client).
const pendingSockets = new Map<string, Promise<ZmxSocket>>();

/** Build a session key from sessionId and optional tabId */
function sessionKey(sessionId: string, tabId: string = "0"): string {
  return `${sessionId}.${tabId}`;
}

/** Set the global terminal output callback (used by connect-client) */
export function setTerminalOutputCallback(callback: ((data: string) => void) | null): void {
  globalTerminalOutputCallback = callback;
}

/** Handle terminal input from the control plane */
export function handleTerminalInput(data: string, sessionId: string = "default"): void {
  writeToTerminal(data, sessionId);
}

/**
 * Ensure a zmx session exists and return the connected socket.
 * Registers output forwarding callbacks on first connect.
 */
async function getOrCreateSocket(sessionId: string, tabId: string = "0"): Promise<ZmxSocket> {
  const key = sessionKey(sessionId, tabId);
  const existing = activeSockets.get(key);
  if (existing) return existing;

  const pending = pendingSockets.get(key);
  if (pending) return pending;

  const creation = (async () => {
    const zmx = getZmxService();
    console.log("[zmx] creating session for", key);
    const sock = await zmx.ensureSession(sessionId, tabId);
    // Frames split multi-byte UTF-8 characters at arbitrary boundaries; a
    // per-socket StringDecoder buffers partial codepoints across frames so
    // clients never see U+FFFD replacement characters.
    const decoder = new StringDecoder("utf8");
    sock.on("frame", (tag: Tag, payload: Buffer) => {
      if (tag !== Tag.Output) return;
      const data = decoder.write(payload);
      if (!data) return; // partial multi-byte character buffered
      for (const cb of terminalOutputCallbacks) {
        cb(data);
      }
      if (globalTerminalOutputCallback) {
        globalTerminalOutputCallback(data);
      }
    });
    sock.on("close", () => {
      console.log("[zmx] socket closed for", key);
      // Flush any partial multi-byte character still buffered in the decoder
      // so trailing output isn't silently dropped when the session ends.
      const remaining = decoder.end();
      if (remaining) {
        for (const cb of terminalOutputCallbacks) {
          cb(remaining);
        }
        if (globalTerminalOutputCallback) {
          globalTerminalOutputCallback(remaining);
        }
      }
      activeSockets.delete(key);
    });
    sock.on("error", (err: Error) => {
      console.error("[zmx] socket error for", key, ":", err.message);
    });
    activeSockets.set(key, sock);
    return sock;
  })();

  pendingSockets.set(key, creation);
  try {
    return await creation;
  } catch (err) {
    console.error("[zmx] failed to create session for", key, ":", err);
    throw err;
  } finally {
    pendingSockets.delete(key);
  }
}

/** Write data to the terminal PTY */
export function writeToTerminal(data: string, sessionId: string = "default"): void {
  const key = sessionKey(sessionId);
  const sock = activeSockets.get(key);
  if (sock) {
    sock.writeInput(data);
  } else {
    getOrCreateSocket(sessionId)
      .then((s) => s.writeInput(data))
      .catch((err) => console.error("[zmx] dropped input, session unavailable:", err?.message ?? err));
  }
}
/** Resize the terminal */
export async function resizeTerminal(cols: number, rows: number, sessionId: string = "default"): Promise<void> {
  const sock = await getOrCreateSocket(sessionId);
  sock.writeResize({ cols, rows });
}

/** Register a callback for terminal output. Returns unregister function. */
export function registerOutputCallback(callback: (data: string) => void): () => void {
  terminalOutputCallbacks.add(callback);
  return () => {
    terminalOutputCallbacks.delete(callback);
  };
}

/** Get terminal history for session restoration */
export async function getTerminalHistory(sessionId: string, tabId: string = "0"): Promise<string> {
  const zmx = getZmxService();
  return zmx.getHistory(sessionId, tabId);
}

/** Initialize a terminal session with size */
export async function initTerminalSession(
  sessionId: string,
  cols: number = 80,
  rows: number = 24,
  tabId: string = "0"
): Promise<void> {
  await getOrCreateSocket(sessionId, tabId);
  await resizeTerminal(cols, rows, sessionId);
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

  // Initialize zmx session
  const sessionId = "default";
  await getOrCreateSocket(sessionId);

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

  // Process input in background
  const inputProcessor = (async () => {
    try {
      for await (const input of requests) {
        if (!streamActive) break;
        if (input.input.case === "data") {
          writeToTerminal(input.input.value, sessionId);
        } else if (input.input.case === "resize") {
          const { cols, rows } = input.input.value;
          await resizeTerminal(cols, rows, sessionId);
        }
      }
    } catch (err) {
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
        await new Promise<void>((resolve) => {
          resolveWait = resolve;
          setTimeout(resolve, 100);
        });
      }
    }
  } finally {
    streamActive = false;
    unregister();
    console.log("[terminal] Terminal stream ended");
  }
}
