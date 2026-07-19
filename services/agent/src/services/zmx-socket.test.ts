import { describe, it, expect, vi, beforeEach } from "vitest";
import { EventEmitter } from "node:events";

/**
 * Fake node:net socket capturing all writes and letting tests inject inbound data.
 */
class FakeSocket extends EventEmitter {
  writes: Buffer[] = [];
  destroyed = false;
  write(buf: Buffer): boolean {
    this.writes.push(Buffer.from(buf));
    return true;
  }
  destroy(): void {
    this.destroyed = true;
    this.emit("close");
  }
}

let fake: FakeSocket;

vi.mock("node:net", () => ({
  createConnection: () => fake,
}));

import { ZmxSocket, Tag, encodeResize } from "./zmx-socket.js";

const HEADER_SIZE = 8; // zmx 0.6.0: packed struct{u8 tag, u32 len} → u40 → sizeOf 8

interface Frame {
  tag: number;
  payload: Buffer;
}

/** Parse zmx-framed bytes using the frozen 8-byte header layout. */
function parseFrames(bufs: Buffer[]): Frame[] {
  const all = Buffer.concat(bufs);
  const frames: Frame[] = [];
  let off = 0;
  while (off + HEADER_SIZE <= all.length) {
    const tag = all[off];
    const len = all.readUInt32LE(off + 1);
    if (off + HEADER_SIZE + len > all.length) break;
    frames.push({ tag, payload: all.subarray(off + HEADER_SIZE, off + HEADER_SIZE + len) });
    off += HEADER_SIZE + len;
  }
  return frames;
}

/** Build an inbound frame the way the zmx daemon would (8-byte header). */
function buildFrame(tag: Tag, payload: Buffer): Buffer {
  const header = Buffer.alloc(HEADER_SIZE);
  header[0] = tag;
  header.writeUInt32LE(payload.length, 1);
  return Buffer.concat([header, payload]);
}

describe("ZmxSocket wire protocol (zmx 0.6.0)", () => {
  beforeEach(() => {
    fake = new FakeSocket();
  });

  it("encodeResize serializes rows first, then cols", () => {
    const buf = encodeResize({ cols: 100, rows: 40 });
    expect(buf.length).toBe(4);
    expect(buf.readUInt16LE(0)).toBe(40); // rows @ offset 0
    expect(buf.readUInt16LE(2)).toBe(100); // cols @ offset 2
  });

  it("sends Init as the first frame with the initial {rows,cols} size", () => {
    new ZmxSocket("/tmp/sock", { cols: 100, rows: 40 });
    const frames = parseFrames(fake.writes);
    expect(frames.length).toBeGreaterThanOrEqual(1);
    expect(frames[0].tag).toBe(Tag.Init);
    expect(frames[0].payload.length).toBe(4);
    expect(frames[0].payload.readUInt16LE(0)).toBe(40);
    expect(frames[0].payload.readUInt16LE(2)).toBe(100);
  });

  it("frames writeInput with tag=Input and an 8-byte header", () => {
    const sock = new ZmxSocket("/tmp/sock");
    fake.writes.length = 0; // discard Init
    sock.writeInput("ab");
    const frames = parseFrames(fake.writes);
    expect(frames).toHaveLength(1);
    expect(frames[0].tag).toBe(Tag.Input);
    expect(frames[0].payload.toString("utf-8")).toBe("ab");
  });

  it("parses an 8-byte-header Output frame from the daemon", () => {
    const sock = new ZmxSocket("/tmp/sock");
    const received: Frame[] = [];
    sock.on("frame", (tag: number, payload: Buffer) => received.push({ tag, payload }));
    fake.emit("data", buildFrame(Tag.Output, Buffer.from("hello")));
    expect(received).toHaveLength(1);
    expect(received[0].tag).toBe(Tag.Output);
    expect(received[0].payload.toString("utf-8")).toBe("hello");
  });

  it("handles a split (partial) frame arriving across two data chunks", () => {
    const sock = new ZmxSocket("/tmp/sock");
    const received: Frame[] = [];
    sock.on("frame", (tag: number, payload: Buffer) => received.push({ tag, payload }));
    const full = buildFrame(Tag.Output, Buffer.from("world"));
    fake.emit("data", full.subarray(0, 4));
    expect(received).toHaveLength(0);
    fake.emit("data", full.subarray(4));
    expect(received).toHaveLength(1);
    expect(received[0].payload.toString("utf-8")).toBe("world");
  });

  it("replies to a daemon Resize request (empty payload) with the last known size", () => {
    const sock = new ZmxSocket("/tmp/sock", { cols: 80, rows: 24 });
    sock.writeResize({ cols: 120, rows: 50 });
    fake.writes.length = 0; // discard Init + explicit resize
    // Daemon asks the (new leader) client for its size with an empty Resize frame.
    fake.emit("data", buildFrame(Tag.Resize, Buffer.alloc(0)));
    const frames = parseFrames(fake.writes);
    expect(frames).toHaveLength(1);
    expect(frames[0].tag).toBe(Tag.Resize);
    expect(frames[0].payload.readUInt16LE(0)).toBe(50); // rows
    expect(frames[0].payload.readUInt16LE(2)).toBe(120); // cols
  });
});
