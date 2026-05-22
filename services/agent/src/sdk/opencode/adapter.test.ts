import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { OpenCodeAdapter } from "./adapter.js";
import { buildOpenCodeAuthContent } from "../auth-materializer.js";
import type { SDKConfig } from "../types.js";
import * as sdk from "@opencode-ai/sdk";
import * as sessionModule from "../../services/session.js";

// ── Helpers ────────────────────────────────────────────────────────────────

vi.mock("@opencode-ai/sdk");

function makeConfig(overrides: Partial<SDKConfig> = {}): SDKConfig {
  return {
    sdkType: "opencode",
    workspaceDir: "/workspace",
    anthropicApiKey: "",
    model: "github-copilot/claude-haiku-4.5",
    githubCopilotOAuthAccessToken: "NETCLODE_PLACEHOLDER_github_copilot_oauth_access",
    githubCopilotOAuthRefreshToken: "NETCLODE_PLACEHOLDER_github_copilot_oauth_refresh",
    githubCopilotOAuthTokenExpires: "1234567890",
    ...overrides,
  };
}

function makeMockServer() {
  return { url: "http://127.0.0.1:4096", close: vi.fn() };
}

/** Create a minimal mock client. Cast through unknown to bypass generic constraints. */
function makeMockClient() {
  return {
    session: {
      create: vi.fn(),
      abort: vi.fn(),
      promptAsync: vi.fn(),
    },
    event: {
      subscribe: vi.fn(),
    },
  } as unknown as sdk.OpencodeClient;
}

/** Shorthand to get the mocked createOpencode. */
function mockCreateOpencode() {
  return vi.mocked(sdk.createOpencode);
}

/** Shorthand to access a mock method on a mock client. */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function mockFn(method: any): ReturnType<typeof vi.fn> {
  return method;
}

/** Create a stream that yields session.idle events (translatable to "result"). */
function makeIdleStream() {
  return (async function* () {
    yield { type: "session.idle" };
  })();
}

/** Create an empty stream (immediately done). */
function makeEmptyStream() {
  return (async function* () {})();
}

// ── Auth materialization (existing tests preserved) ────────────────────────

describe("OpenCode auth materialization", () => {
  it("builds auth.json with the Copilot OAuth values provided in session config", () => {
    const authContent = buildOpenCodeAuthContent(makeConfig());
    expect(authContent).toEqual({
      "github-copilot": {
        type: "oauth",
        refresh: "NETCLODE_PLACEHOLDER_github_copilot_oauth_refresh",
        access: "NETCLODE_PLACEHOLDER_github_copilot_oauth_access",
        expires: 1234567890,
      },
    });
    expect(JSON.stringify(authContent)).toContain("NETCLODE_PLACEHOLDER_github_copilot_oauth_access");
  });

  it("does not build auth.json for non-Copilot OpenCode models", () => {
    expect(buildOpenCodeAuthContent(makeConfig({ model: "anthropic/claude-sonnet-4-5" }))).toBeNull();
  });
});

// ── Server lifecycle ───────────────────────────────────────────────────────

describe("OpenCodeAdapter server lifecycle", () => {
  let adapter: OpenCodeAdapter;
  let mockServer: ReturnType<typeof makeMockServer>;

  beforeEach(() => {
    vi.clearAllMocks();
    mockServer = makeMockServer();
    mockCreateOpencode().mockResolvedValue({
      client: makeMockClient(),
      server: mockServer,
    });
    adapter = new OpenCodeAdapter();
  });

  it("starts server with createOpencode on initialize", async () => {
    await adapter.initialize(makeConfig({ model: "anthropic/claude-sonnet-4-5" }));

    expect(sdk.createOpencode).toHaveBeenCalledTimes(1);
    const callArgs = mockCreateOpencode().mock.calls[0][0];
    expect(callArgs).toBeDefined();
    expect(callArgs!.hostname).toBe("127.0.0.1");
    expect(callArgs!.port).toBe(4096);
    expect(callArgs!.timeout).toBe(60_000);
    expect(callArgs!.config).toBeDefined();
    expect((callArgs!.config as Record<string, unknown>).model).toBe("anthropic/claude-sonnet-4-5");
  });

  it("passes permission config to createOpencode", async () => {
    await adapter.initialize(makeConfig({ model: "anthropic/claude-sonnet-4-5" }));

    const callArgs = mockCreateOpencode().mock.calls[0][0];
    const cfg = callArgs!.config as Record<string, unknown>;
    const perm = cfg.permission as Record<string, string>;
    expect(perm.edit).toBe("allow");
    expect(perm.bash).toBe("allow");
    expect(perm.mcp).toBe("allow");
    expect(perm.question).toBe("deny");
  });

  it("passes ollama provider config when ollamaUrl is set", async () => {
    await adapter.initialize(makeConfig({
      model: "ollama/qwen3-coder",
      ollamaUrl: "http://ollama:11434",
    }));

    const callArgs = mockCreateOpencode().mock.calls[0][0];
    const cfg = callArgs!.config as Record<string, unknown>;
    const provider = cfg.provider as Record<string, unknown>;
    expect(provider).toBeDefined();
    expect(provider.ollama).toBeDefined();
    const ollama = provider.ollama as Record<string, unknown>;
    expect(ollama.npm).toBe("@ai-sdk/openai-compatible");
    expect(ollama.options).toEqual({ baseURL: "http://ollama:11434/v1" });
  });

  it("adds /v1 suffix to ollama URL when not present", async () => {
    await adapter.initialize(makeConfig({
      model: "ollama/qwen3-coder",
      ollamaUrl: "http://ollama:11434/",
    }));

    const callArgs = mockCreateOpencode().mock.calls[0][0];
    const cfg = callArgs!.config as Record<string, unknown>;
    const provider = cfg.provider as Record<string, unknown>;
    const ollama = provider.ollama as Record<string, unknown>;
    expect(ollama.options).toEqual({ baseURL: "http://ollama:11434/v1" });
  });

  it("does not restart if already running", async () => {
    await adapter.initialize(makeConfig({ model: "anthropic/claude-sonnet-4-5" }));
    expect(sdk.createOpencode).toHaveBeenCalledTimes(1);

    // Second initialize should reuse existing server
    await adapter.initialize(makeConfig({ model: "anthropic/claude-sonnet-4-5" }));
    expect(sdk.createOpencode).toHaveBeenCalledTimes(1);
  });
});

// ── Session operations ─────────────────────────────────────────────────────

describe("OpenCodeAdapter session operations", () => {
  let adapter: OpenCodeAdapter;
  let mockServer: ReturnType<typeof makeMockServer>;
  let mockClient: ReturnType<typeof makeMockClient>;

  beforeEach(async () => {
    vi.clearAllMocks();
    // Clear persistent session mapping between tests to avoid cross-test pollution
    sessionModule.loadSessionMapping();
    mockServer = makeMockServer();
    mockClient = makeMockClient();
    mockFn(mockClient.session.create).mockResolvedValue({ data: { id: "oc-session-1" } });
    mockFn(mockClient.session.promptAsync).mockResolvedValue(undefined);
    mockFn(mockClient.event.subscribe).mockResolvedValue({ stream: makeIdleStream() });

    mockCreateOpencode().mockResolvedValue({ client: mockClient, server: mockServer });
    adapter = new OpenCodeAdapter();
    await adapter.initialize(makeConfig({ model: "anthropic/claude-sonnet-4-5" }));
  });

  it("creates session via client.session.create()", async () => {
    const events = adapter.executePrompt("session-ops-1", "hello");
    const results: unknown[] = [];
    for await (const e of events) results.push(e);

    expect(mockFn(mockClient.session.create)).toHaveBeenCalledWith({
      query: { directory: "/agent/workspace" },
    });
    expect(results.length).toBeGreaterThan(0);
    expect(results[0]).toMatchObject({ type: "result" });
  });

  it("reuses existing session mapping on subsequent calls", async () => {
    const first = adapter.executePrompt("session-ops-2", "first");
    const firstResults: unknown[] = [];
    for await (const e of first) firstResults.push(e);

    expect(mockFn(mockClient.session.create)).toHaveBeenCalledTimes(1);

    // Second call reuses session
    mockFn(mockClient.event.subscribe).mockResolvedValue({ stream: makeIdleStream() });
    const second = adapter.executePrompt("session-ops-2", "second");
    const secondResults: unknown[] = [];
    for await (const e of second) secondResults.push(e);

    expect(mockFn(mockClient.session.create)).toHaveBeenCalledTimes(1);
    expect(secondResults.length).toBeGreaterThan(0);
  });

  it("passes directory to all SDK calls", async () => {
    const events = adapter.executePrompt("session-ops-3", "test");
    const results: unknown[] = [];
    for await (const e of events) results.push(e);

    expect(mockFn(mockClient.session.create)).toHaveBeenCalledWith(
      expect.objectContaining({ query: { directory: "/agent/workspace" } }),
    );
    expect(mockFn(mockClient.event.subscribe)).toHaveBeenCalledWith(
      expect.objectContaining({ query: { directory: "/agent/workspace" } }),
    );
  });
});

// ── Event streaming ────────────────────────────────────────────────────────

describe("OpenCodeAdapter event streaming", () => {
  let adapter: OpenCodeAdapter;
  let mockServer: ReturnType<typeof makeMockServer>;
  let mockClient: ReturnType<typeof makeMockClient>;

  beforeEach(async () => {
    vi.clearAllMocks();
    mockServer = makeMockServer();
    mockClient = makeMockClient();
    mockFn(mockClient.session.create).mockResolvedValue({ data: { id: "oc-session-1" } });
    mockFn(mockClient.session.promptAsync).mockResolvedValue(undefined);
    mockCreateOpencode().mockResolvedValue({ client: mockClient, server: mockServer });
    adapter = new OpenCodeAdapter();
    await adapter.initialize(makeConfig({ model: "anthropic/claude-sonnet-4-5" }));
  });

  it("subscribes to events via client.event.subscribe()", async () => {
    mockFn(mockClient.event.subscribe).mockResolvedValue({ stream: makeIdleStream() });

    const events = adapter.executePrompt("session-ev-1", "test");
    const results: unknown[] = [];
    for await (const e of events) results.push(e);

    expect(mockFn(mockClient.event.subscribe)).toHaveBeenCalledTimes(1);
    expect(results.length).toBeGreaterThan(0);
  });

  it("yields prompt events from stream", async () => {
    mockFn(mockClient.event.subscribe).mockResolvedValue({
      stream: (async function* () {
        yield { type: "session.idle" };
        yield { type: "session.idle" };
      })(),
    });

    const events = adapter.executePrompt("session-ev-2", "test");
    const results: unknown[] = [];
    for await (const e of events) results.push(e);

    // Two session.idle → two "result" events, plus thinking finalization
    expect(results.length).toBeGreaterThanOrEqual(2);
  });

  it("handles stream completion cleanly", async () => {
    mockFn(mockClient.event.subscribe).mockResolvedValue({ stream: makeEmptyStream() });

    const events = adapter.executePrompt("session-ev-3", "test");
    const results: unknown[] = [];
    for await (const e of events) results.push(e);

    // Empty stream produces nothing
    expect(results.length).toBe(0);
  });

  it("handles stream errors gracefully", async () => {
    mockFn(mockClient.event.subscribe).mockImplementation(() =>
      Promise.reject(new Error("Connection refused")),
    );

    const events = adapter.executePrompt("session-ev-4", "test");
    const results: unknown[] = [];
    for await (const e of events) results.push(e);

    expect(results.length).toBeGreaterThan(0);
    expect(results[0]).toMatchObject({ type: "error", retryable: false });
  });

  it("sends prompt via client.session.promptAsync()", async () => {
    mockFn(mockClient.event.subscribe).mockResolvedValue({ stream: makeIdleStream() });

    const events = adapter.executePrompt("session-ev-5", "build me a button");
    const results: unknown[] = [];
    for await (const e of events) results.push(e);

    expect(mockFn(mockClient.session.promptAsync)).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { id: "oc-session-1" },
        body: expect.objectContaining({
          parts: [{ type: "text", text: "build me a button" }],
        }),
      }),
    );
  });
});

// ── Interrupt ──────────────────────────────────────────────────────────────

describe("OpenCodeAdapter interrupt", () => {
  let adapter: OpenCodeAdapter;
  let mockServer: ReturnType<typeof makeMockServer>;
  let mockClient: ReturnType<typeof makeMockClient>;

  beforeEach(async () => {
    vi.clearAllMocks();
    mockServer = makeMockServer();
    mockClient = makeMockClient();
    mockFn(mockClient.session.create).mockResolvedValue({ data: { id: "oc-session-1" } });
    mockFn(mockClient.session.promptAsync).mockResolvedValue(undefined);
    mockCreateOpencode().mockResolvedValue({ client: mockClient, server: mockServer });
    adapter = new OpenCodeAdapter();
    await adapter.initialize(makeConfig({ model: "anthropic/claude-sonnet-4-5" }));
  });

  it("aborts session via client.session.abort() on interrupt", async () => {
    mockFn(mockClient.event.subscribe).mockResolvedValue({
      stream: (async function* () {
        // Yield events so the for-await loop body runs (and checks interrupt)
        for (let i = 0; i < 100; i++) {
          yield { type: "session.idle" };
          await new Promise((r) => setTimeout(r, 2));
        }
      })(),
    });

    const events = adapter.executePrompt("session-int-1", "test");

    // Trigger interrupt after a tick
    setTimeout(() => {
      adapter.setInterruptSignal();
    }, 10);

    const results: unknown[] = [];
    for await (const e of events) results.push(e);

    expect(mockFn(mockClient.session.abort)).toHaveBeenCalledWith({
      path: { id: "oc-session-1" },
      query: { directory: "/agent/workspace" },
    });
    expect(results).toContainEqual({ type: "system", message: "interrupted" });
  });

  it("sets interrupt signal", () => {
    expect(adapter.isInterrupted()).toBe(false);
    adapter.setInterruptSignal();
    expect(adapter.isInterrupted()).toBe(true);
  });

  it("clears interrupt signal", () => {
    adapter.setInterruptSignal();
    expect(adapter.isInterrupted()).toBe(true);
    adapter.clearInterruptSignal();
    expect(adapter.isInterrupted()).toBe(false);
  });
});

// ── Shutdown ───────────────────────────────────────────────────────────────

describe("OpenCodeAdapter shutdown", () => {
  let adapter: OpenCodeAdapter;
  let mockServer: ReturnType<typeof makeMockServer>;

  beforeEach(async () => {
    vi.clearAllMocks();
    mockServer = makeMockServer();
    mockCreateOpencode().mockResolvedValue({
      client: makeMockClient(),
      server: mockServer,
    });
    adapter = new OpenCodeAdapter();
    await adapter.initialize(makeConfig({ model: "anthropic/claude-sonnet-4-5" }));
  });

  it("calls server.close() on shutdown", async () => {
    await adapter.shutdown();
    expect(mockServer.close).toHaveBeenCalledTimes(1);
  });

  it("is idempotent (second shutdown is no-op)", async () => {
    await adapter.shutdown();
    expect(mockServer.close).toHaveBeenCalledTimes(1);
    await adapter.shutdown();
    expect(mockServer.close).toHaveBeenCalledTimes(1);
  });
});
