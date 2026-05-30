/**
 * zmx binary protocol framing over Unix sockets.
 *
 * Frame: [1-byte Tag][4-byte Len (u32 LE)][Payload...]
 *
 * Protocol source: https://github.com/neurosnap/zmx/blob/main/src/ipc.zig
 */

import { type Socket, createConnection } from "node:net";
import { EventEmitter } from "node:events";

/** zmx IPC protocol tags */
export const enum Tag {
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

const HEADER_SIZE = 5; // 1 byte tag + 4 bytes len (u32 LE)

/** Resize payload: cols:u16, rows:u16 (4 bytes total) */
export interface Resize {
  cols: number;
  rows: number;
}

export function encodeResize(resize: Resize): Buffer {
  const buf = Buffer.alloc(4);
  buf.writeUInt16LE(resize.cols, 0);
  buf.writeUInt16LE(resize.rows, 2);
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

  constructor(socketPath: string) {
    super();
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
  }

  private onData(chunk: Buffer): void {
    this.buffer = Buffer.concat([this.buffer, chunk]);
    while (this.buffer.length >= HEADER_SIZE) {
      const tag = this.buffer[0] as Tag;
      const len = this.buffer.readUInt32LE(1);
      if (this.buffer.length < HEADER_SIZE + len) break;
      const payload = this.buffer.subarray(HEADER_SIZE, HEADER_SIZE + len);
      this.buffer = this.buffer.subarray(HEADER_SIZE + len);
      this.emit("frame", tag, payload);
    }
  }

  /** Write a framed message to the socket */
  writeFrame(tag: Tag, payload: Buffer = Buffer.alloc(0)): void {
    if (this.closed) return;
    const header = Buffer.alloc(HEADER_SIZE);
    header[0] = tag;
    header.writeUInt32LE(payload.length, 1);
    this.socket.write(Buffer.concat([header, payload]));
  }

  /** Send terminal input (keystrokes) */
  writeInput(data: string): void {
    this.writeFrame(Tag.Input, Buffer.from(data, "utf-8"));
  }

  /** Resize the terminal */
  writeResize(resize: Resize): void {
    this.writeFrame(Tag.Resize, encodeResize(resize));
  }

  /** Close the socket connection */
  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.socket.destroy();
  }
}
