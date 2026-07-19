import { describe, it, expect, vi, beforeEach } from "vitest";

const { spawnMock, existsSyncMock, mkdirSyncMock, execMock } = vi.hoisted(() => ({
  spawnMock: vi.fn((..._args: unknown[]) => ({ unref: vi.fn() })),
  existsSyncMock: vi.fn(),
  mkdirSyncMock: vi.fn(),
  execMock: vi.fn(),
}));

vi.mock("node:child_process", () => ({
  spawn: spawnMock,
  exec: execMock,
}));

const { rmSyncMock } = vi.hoisted(() => ({ rmSyncMock: vi.fn() }));

vi.mock("node:fs", () => ({
  existsSync: existsSyncMock,
  mkdirSync: mkdirSyncMock,
  rmSync: rmSyncMock,
}));

// Fake ZmxSocket so we never touch a real Unix socket. `readyOutcomes`
// scripts the async connection result per construction (default: connected).
const socketInstances: Array<{ path: string; size: unknown }> = [];
const readyOutcomes: Array<Promise<void>> = [];
vi.mock("./zmx-socket.js", () => ({
  Tag: { Output: 1, Kill: 5, Detach: 3, DetachAll: 4 },
  ZmxSocket: class {
    ready: Promise<void>;
    constructor(path: string, size?: unknown) {
      socketInstances.push({ path, size });
      this.ready = readyOutcomes.shift() ?? Promise.resolve();
      this.ready.catch(() => {});
    }
    writeResize() {}
    writeFrame() {}
    close() {}
    on() {}
  },
}));

import { ZmxService } from "./zmx-service.js";

describe("ZmxService.ensureSession", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    socketInstances.length = 0;
    readyOutcomes.length = 0;
  });

  it("removes a stale socket file and respawns when the connection fails", async () => {
    // Daemon socket file exists on disk, but the daemon is gone: the first
    // socket's async connect rejects. Expect: file removed, daemon spawned,
    // second socket connects.
    existsSyncMock.mockReturnValue(true);
    readyOutcomes.push(Promise.reject(new Error("ECONNREFUSED")));
    readyOutcomes.push(Promise.resolve());

    const svc = new ZmxService();
    const sock = await svc.ensureSession("sess-stale");

    expect(rmSyncMock).toHaveBeenCalledWith(
      expect.stringContaining("netclode.sess-stale.0"),
      { force: true },
    );
    expect(spawnMock).toHaveBeenCalledTimes(1);
    expect(socketInstances).toHaveLength(2);
    expect(sock).toBeDefined();
  });

  it("spawns a detached `zmx attach <netclode.session.tab>` daemon and connects a socket", async () => {
    // First existsSync (daemon-on-disk check) → false so we spawn;
    // subsequent existsSync (waitForSocket poll) → true so it resolves.
    existsSyncMock.mockReturnValueOnce(false).mockReturnValue(true);

    const svc = new ZmxService();
    await svc.ensureSession("sess-123");

    expect(spawnMock).toHaveBeenCalledTimes(1);
    const [cmd, args, opts] = spawnMock.mock.calls[0];
    expect(cmd).toBe("zmx");
    expect(args).toEqual(["attach", "netclode.sess-123.0"]);
    expect(opts).toMatchObject({ detached: true, stdio: "ignore" });
    // The daemon's shell inherits this env — TERM must be pinned so
    // clear/less/vim and bash line editing work inside the VM.
    expect((opts as { env: Record<string, string> }).env.TERM).toBeTruthy();
    expect(socketInstances).toHaveLength(1);
  });

  it("reuses an existing daemon socket without spawning a new process", async () => {
    // Daemon socket already present on disk → no spawn.
    existsSyncMock.mockReturnValue(true);

    const svc = new ZmxService();
    await svc.ensureSession("sess-existing");

    expect(spawnMock).not.toHaveBeenCalled();
    expect(socketInstances).toHaveLength(1);
  });
});

describe("ZmxService.getHistory (restore source)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("returns the VT scrollback dump on success (restore content)", async () => {
    execMock.mockImplementation((_cmd: string, _opts: unknown, cb: (e: Error | null, out: string) => void) => {
      cb(null, "\x1b[2Jrestored-screen");
    });
    const svc = new ZmxService();
    const history = await svc.getHistory("sess-abc");
    expect(history).toBe("\x1b[2Jrestored-screen");
    const cmd = execMock.mock.calls[0][0] as string;
    expect(cmd).toContain("zmx history netclode.sess-abc.0 --vt");
  });

  it("resolves to empty string when the session has no history (nothing to replay)", async () => {
    execMock.mockImplementation((_cmd: string, _opts: unknown, cb: (e: Error | null, out: string) => void) => {
      cb(new Error("session not found"), "");
    });
    const svc = new ZmxService();
    const history = await svc.getHistory("missing-sess");
    expect(history).toBe("");
  });
});
