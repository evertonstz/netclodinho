import { describe, expect, it, vi } from "vitest";
import {
  ComposedNetclodeAgent,
  createGitInspector,
  createSessionBootstrapper,
  createTitleGenerator,
  StreamTimeoutError,
  withTimeout,
} from "./runtime.js";
import { UnsupportedAgentCapabilityError, createAgentCapabilities, type NetclodePromptBackend, type PromptEvent } from "./types.js";
import type { RepositoryContext, AgentGitFileChange } from "./types.js";

// Override timeout constants before module evaluation
vi.mock("./runtime.js", async (importOriginal) => {
  const mod = await importOriginal<typeof import("./runtime.js")>();
  return { ...mod };
});

function createFakeBackend(overrides: Partial<NetclodePromptBackend> = {}): NetclodePromptBackend {
  return {
    capabilities: createAgentCapabilities({ interrupt: true }),
    async initialize() {},
    async *executePrompt(): AsyncGenerator<PromptEvent> {
      yield { type: "system", message: "backend" };
    },
    async interrupt() {},
    async shutdown() {},
    ...overrides,
  };
}

describe("ComposedNetclodeAgent", () => {
  it("combines backend and shared-service capabilities", () => {
    const agent = new ComposedNetclodeAgent(createFakeBackend({
      capabilities: createAgentCapabilities({ interrupt: true, toolStreaming: true, thinkingStreaming: false }),
    }), {
      titleGenerator: createTitleGenerator(async () => "title"),
      gitInspector: {
        getGitStatus: async () => [],
        getGitDiff: async () => "",
      },
    });

    expect(agent.capabilities).toEqual(createAgentCapabilities({
      interrupt: true,
      titleGeneration: true,
      gitStatus: true,
      gitDiff: true,
      toolStreaming: true,
      thinkingStreaming: false,
    }));
  });

  it("runs session bootstrap before backend prompt execution", async () => {
    const events: string[] = [];
    const agent = new ComposedNetclodeAgent(createFakeBackend({
      async *executePrompt(): AsyncGenerator<PromptEvent> {
        events.push("backend");
        yield { type: "system", message: "backend" };
      },
    }), {
      sessionBootstrapper: createSessionBootstrapper(async function* () {
        events.push("bootstrap");
        yield { type: "repoClone", stage: "done", repo: "repo", message: "ok" };
      }),
    });

    const output: PromptEvent[] = [];
    for await (const event of agent.executePrompt("sess-1", "hello", { repos: ["owner/repo"] })) {
      output.push(event);
    }

    expect(events).toEqual(["bootstrap", "backend"]);
    expect(output.map((event) => event.type)).toEqual(["repoClone", "system"]);
  });

  it("throws a typed error when title generation is unsupported", async () => {
    const agent = new ComposedNetclodeAgent(createFakeBackend());
    await expect(agent.generateTitle("hello")).rejects.toBeInstanceOf(UnsupportedAgentCapabilityError);
  });

  it("forwards interrupt to the backend", async () => {
    const interrupt = vi.fn(async () => {});
    const agent = new ComposedNetclodeAgent(createFakeBackend({ interrupt }));
    await agent.interrupt();
    expect(interrupt).toHaveBeenCalledOnce();
  });
});

describe("createGitInspector", () => {
  it("prefixes multi-repo git status paths", async () => {
    const inspector = createGitInspector("/workspace", {
      readGitStatus: vi.fn(async (dir: string) => {
        if (dir.endsWith("owner__repo")) {
          return [{ path: "file.ts", status: "modified" as const, staged: false }];
        }
        return [];
      }),
      readGitDiff: vi.fn(async () => ""),
    });

    const files = await inspector.getGitStatus({ repos: ["github.com/owner/repo.git", "github.com/other/two.git"] });
    expect(files).toContainEqual(expect.objectContaining({ path: "owner__repo/file.ts", repo: "github.com/owner/repo.git" }));
  });

  it("routes git diff for a prefixed file to the matching repo", async () => {
    const readGitDiff = vi.fn(async (_dir: string, file?: string, prefix?: string) => `${prefix}:${file}`);
    const inspector = createGitInspector("/workspace", {
      readGitStatus: vi.fn(async () => []),
      readGitDiff,
    });

    const diff = await inspector.getGitDiff(
      { repos: ["github.com/owner/repo.git", "github.com/other/two.git"] },
      "owner__repo/src/main.ts",
    );

    expect(diff).toBe("owner__repo:src/main.ts");
    expect(readGitDiff).toHaveBeenCalledOnce();
  });
});

describe("withTimeout", () => {
  it("yields events normally for a clean stream", async () => {
    async function* source(): AsyncGenerator<string> {
      yield "a";
      yield "b";
    }
    const results: string[] = [];
    for await (const val of withTimeout(source(), 100)) {
      results.push(val);
    }
    expect(results).toEqual(["a", "b"]);
  });

  it("throws StreamTimeoutError when stream stalls", async () => {
    async function* source(): AsyncGenerator<string> {
      await new Promise(() => {}); // never resolves
      yield "too-late";
    }
    const iter = withTimeout(source(), 10)[Symbol.asyncIterator]();
    await expect(iter.next()).rejects.toBeInstanceOf(StreamTimeoutError);
  });

  it("propagates non-timeout errors", async () => {
    async function* source(): AsyncGenerator<string> {
      yield "ok";
      throw new Error("boom");
    }
    const results: string[] = [];
    await expect(async () => {
      for await (const val of withTimeout(source(), 100)) {
        results.push(val);
      }
    }).rejects.toThrow("boom");
    expect(results).toEqual(["ok"]);
  });
});

describe("ComposedNetclodeAgent stream timeout", () => {
  it("reconnects on timeout for streamReconnect-capable backends", async () => {
    let callCount = 0;
    const backend = createFakeBackend({
      capabilities: createAgentCapabilities({ interrupt: true, streamReconnect: true }),
      async *executePrompt(): AsyncGenerator<PromptEvent> {
        callCount++;
        if (callCount === 1) {
          // Simulate hanging stream by throwing StreamTimeoutError directly
          throw new StreamTimeoutError();
        }
        yield { type: "system", message: "recovered" };
      },
    });

    const agent = new ComposedNetclodeAgent(backend);
    const events: PromptEvent[] = [];
    for await (const event of agent.executePrompt("s", "hi")) {
      events.push(event);
    }

    expect(callCount).toBe(2);
    expect(events.map((e) => e.type)).toEqual(["system", "system"]);
    expect(events[0]).toMatchObject({ type: "system", message: expect.stringContaining("Reconnecting") });
    expect(events[1]).toMatchObject({ type: "system", message: "recovered" });
  });

  it("yields error without retry for non-reconnect backends on timeout", async () => {
    let callCount = 0;
    const backend = createFakeBackend({
      capabilities: createAgentCapabilities({ interrupt: true, streamReconnect: false }),
      async *executePrompt(): AsyncGenerator<PromptEvent> {
        callCount++;
        throw new StreamTimeoutError();
      },
    });

    const agent = new ComposedNetclodeAgent(backend);
    const events: PromptEvent[] = [];
    for await (const event of agent.executePrompt("s", "hi")) {
      events.push(event);
    }

    expect(events.length).toBe(1);
    expect(events[0].type).toBe("error");
    expect(callCount).toBe(1);
  });

  it("exhausts retries and yields error after max reconnects", async () => {
    let callCount = 0;
    const backend = createFakeBackend({
      capabilities: createAgentCapabilities({ interrupt: true, streamReconnect: true }),
      async *executePrompt(): AsyncGenerator<PromptEvent> {
        callCount++;
        throw new StreamTimeoutError();
      },
    });

    const agent = new ComposedNetclodeAgent(backend);
    const events: PromptEvent[] = [];
    for await (const event of agent.executePrompt("s", "hi")) {
      events.push(event);
    }

    // MAX_RECONNECTS=3 → 4 attempts (1 initial + 3 retries)
    expect(callCount).toBe(4);
    const lastEvent = events[events.length - 1];
    expect(lastEvent.type).toBe("error");
  });

  it("propagates non-timeout errors through executePrompt", async () => {
    const backend = createFakeBackend({
      capabilities: createAgentCapabilities({ interrupt: true, streamReconnect: true }),
      async *executePrompt(): AsyncGenerator<PromptEvent> {
        yield { type: "system", message: "first" };
        throw new Error("SDK internal error");
      },
    });

    const agent = new ComposedNetclodeAgent(backend);
    const events: PromptEvent[] = [];
    await expect(async () => {
      for await (const event of agent.executePrompt("s", "hi")) {
        events.push(event);
      }
    }).rejects.toThrow("SDK internal error");
    expect(events.map((e) => e.type)).toEqual(["system"]);
  });
});
