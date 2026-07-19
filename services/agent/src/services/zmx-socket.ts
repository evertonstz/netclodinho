/**
 * zmx binary protocol framing over Unix sockets.
 *
 * Frame header is a Zig `packed struct { tag: u8, len: u32 }`. That packs to a
 * u40 backing integer whose `@sizeOf` rounds up to 8 bytes, so the wire header
 * is 8 bytes — NOT 5:
 *
 *   byte 0      : tag (u8)
 *   bytes 1..4  : len (u32, little-endian)
 *   bytes 5..7  : zero padding
 *   bytes 8..   : payload
 *
 * The `Resize`/`Init` payload is a `packed struct { rows: u16, cols: u16 }`
 * (rows FIRST), 4 bytes little-endian.
 *
 * A client must send an `Init` frame carrying its terminal size before the
 * daemon treats it as a real terminal client (sets it as leader, sizes the PTY,
 * and — on re-attach — replays serialized screen state as an `Output` frame).
 *
 * Protocol source: https://github.com/neurosnap/zmx (src/ipc.zig, src/main.zig, v0.6.0)
 */

import { type Socket, createConnection } from "node:net";
import { EventEmitter } from "node:events";

/** zmx IPC protocol tags (wire values are frozen in zmx ipc.zig) */
export enum Tag {
  Input = 0,
  Output = 1,
  Resize = 2,
  Detach = 3,
  DetachAll = 4,
  Kill = 5,
  Info = 6,
  Init = 7,
  History = 8,
  Run = 9,
  Ack = 10,
  Switch = 11,
  Write = 12,
  TaskComplete = 13,
}

/** 1-byte tag + u32 len, but the Zig packed struct rounds up to 8 bytes on the wire. */
const HEADER_SIZE = 8;

/** Resize payload: rows:u16, cols:u16 (4 bytes total, little-endian, rows first) */
export interface Resize {
  cols: number;
  rows: number;
}

const DEFAULT_SIZE: Resize = { cols: 80, rows: 24 };

export function encodeResize(resize: Resize): Buffer {
  const buf = Buffer.alloc(4);
  buf.writeUInt16LE(resize.rows, 0);
  buf.writeUInt16LE(resize.cols, 2);
  return buf;
}

/**
 * Socket client for a zmx daemon session.
 * Handles binary framing and emits parsed frames.
 */
export class ZmxSocket extends EventEmitter {
  private socket: Socket;
  private buffer = Buffer.alloc(0);
  private closed = false;
  private lastSize: Resize;

  /** Resolves once the underlying socket connects; rejects on connection error. */
  readonly ready: Promise<void>;

  constructor(socketPath: string, initSize: Resize = DEFAULT_SIZE) {
    super();
    this.lastSize = { ...initSize };
    this.socket = createConnection(socketPath);
    this.socket.on("data", (chunk: Buffer) => this.onData(chunk));
    this.socket.on("close", () => {
      this.closed = true;
      this.emit("close");
    });
    this.socket.on("error", (err) => {
      this.closed = true;
      this.emit("error", err);
    });

    // Connection readiness: createConnection errors are ASYNC, so a plain
    // try/catch around the constructor can never detect a stale socket file.
    // Callers await `ready` to distinguish a live daemon from a leftover
    // socket whose daemon is gone.
    this.ready = new Promise<void>((resolve, reject) => {
      this.socket.once("connect", () => resolve());
      this.socket.once("error", (err) => reject(err));
    });
    // Don't surface an unhandled rejection when nobody awaits `ready`
    // and the socket errors later in its life.
    this.ready.catch(() => {});

    // The Init handshake MUST be the first frame the daemon sees so it registers
    // this connection as a real terminal client (leader + PTY size + restore
    // snapshot). node buffers writes until the socket connects, preserving order.
    this.writeInit(this.lastSize);
  }

  private onData(chunk: Buffer): void {
    this.buffer = Buffer.concat([this.buffer, chunk]);
    while (this.buffer.length >= HEADER_SIZE) {
      const tag = this.buffer[0] as Tag;
      const len = this.buffer.readUInt32LE(1);
      if (this.buffer.length < HEADER_SIZE + len) break;
      const payload = this.buffer.subarray(HEADER_SIZE, HEADER_SIZE + len);
      this.buffer = this.buffer.subarray(HEADER_SIZE + len);

      // The daemon asks a newly-promoted leader for its window size by sending
      // an empty Resize frame; answer it so the PTY gets the right dimensions.
      if (tag === Tag.Resize && payload.length === 0) {
        this.writeResize(this.lastSize);
      }

      this.emit("frame", tag, payload);
    }
  }

  /** Write a framed message to the socket (8-byte header + payload) */
  writeFrame(tag: Tag, payload: Buffer = Buffer.alloc(0)): void {
    if (this.closed) return;
    const header = Buffer.alloc(HEADER_SIZE); // bytes 5..7 remain zero padding
    header[0] = tag;
    header.writeUInt32LE(payload.length, 1);
    this.socket.write(Buffer.concat([header, payload]));
  }

  /** Register as a terminal client with an initial size (must be the first frame) */
  writeInit(resize: Resize): void {
    this.lastSize = { ...resize };
    this.writeFrame(Tag.Init, encodeResize(resize));
  }

  /** Send terminal input (keystrokes) */
  writeInput(data: string): void {
    this.writeFrame(Tag.Input, Buffer.from(data, "utf-8"));
  }

  /** Resize the terminal */
  writeResize(resize: Resize): void {
    this.lastSize = { ...resize };
    this.writeFrame(Tag.Resize, encodeResize(resize));
  }

  /** Close the socket connection */
  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.socket.destroy();
  }
}
