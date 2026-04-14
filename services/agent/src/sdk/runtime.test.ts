import { describe, expect, it, vi } from "vitest";
import {
  ComposedNetclodeAgent,
  createGitInspector,
  createSessionBootstrapper,
  createTitleGenerator,
} from "./runtime.js";
import { UnsupportedAgentCapabilityError, createAgentCapabilities, type NetclodePromptBackend, type PromptEvent } from "./types.js";
import type { RepositoryContext, AgentGitFileChange } from "./types.js";

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
