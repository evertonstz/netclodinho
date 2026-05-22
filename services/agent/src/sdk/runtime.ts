import {
  getGitStatus as readGitStatus,
  getGitDiff as readGitDiff,
  getRepoPath,
  getRepoPrefix,
} from "../git.js";
import { initializeSessionRepos as defaultInitializeSessionRepos } from "../services/session.js";
import { generateTitle as defaultGenerateTitle } from "../services/title.js";
import { WORKSPACE_DIR } from "../constants.js";
import type {
  AgentCapabilities,
  AgentGitFileChange,
  GitInspector,
  NetclodeAgent,
  NetclodePromptBackend,
  PromptConfig,
  RepositoryContext,
  SessionBootstrapper,
  TitleGenerator,
} from "./types.js";
import { UnsupportedAgentCapabilityError, createAgentCapabilities } from "./types.js";

export interface NetclodeAgentDependencies {
  titleGenerator?: TitleGenerator;
  gitInspector?: GitInspector;
  sessionBootstrapper?: SessionBootstrapper;
}

interface RepoConfig {
  repo: string;
  dir: string;
  prefix: string;
}

function resolveRepoConfigs(repos: string[] = [], workspaceDir: string): RepoConfig[] {
  const filteredRepos = repos.filter(Boolean);
  const totalRepos = filteredRepos.length;

  return filteredRepos.map((repo) => ({
    repo,
    dir: getRepoPath(repo, totalRepos, workspaceDir),
    prefix: getRepoPrefix(repo, totalRepos),
  }));
}

export interface GitInspectorDependencies {
  readGitStatus?: typeof readGitStatus;
  readGitDiff?: typeof readGitDiff;
}

export function createGitInspector(
  workspaceDir: string = WORKSPACE_DIR,
  dependencies: GitInspectorDependencies = {},
): GitInspector {
  const gitStatus = dependencies.readGitStatus ?? readGitStatus;
  const gitDiff = dependencies.readGitDiff ?? readGitDiff;

  return {
    async getGitStatus(context?: RepositoryContext): Promise<AgentGitFileChange[]> {
      const repoConfigs = resolveRepoConfigs(context?.repos, workspaceDir);
      const files: AgentGitFileChange[] = [];

      if (repoConfigs.length === 0) {
        const rootFiles = await gitStatus(workspaceDir);
        files.push(...rootFiles.map((file) => ({ ...file, repo: "" })));
        return files;
      }

      for (const { repo, dir, prefix } of repoConfigs) {
        const repoFiles = await gitStatus(dir);
        files.push(
          ...repoFiles.map((file) => ({
            ...file,
            path: prefix ? `${prefix}/${file.path}` : file.path,
            repo,
          })),
        );
      }

      return files;
    },

    async getGitDiff(context?: RepositoryContext, file?: string): Promise<string> {
      const repoConfigs = resolveRepoConfigs(context?.repos, workspaceDir);

      if (repoConfigs.length === 0) {
        return gitDiff(workspaceDir, file);
      }

      if (file) {
        const parts = file.split("/");
        const prefix = parts[0];
        let target = repoConfigs.find((repoConfig) => repoConfig.prefix === prefix);
        let relativeFile = parts.slice(1).join("/");

        if (!target && repoConfigs.length === 1) {
          target = repoConfigs[0];
          relativeFile = file;
        }

        if (!target) {
          return "";
        }

        return gitDiff(target.dir, relativeFile.length > 0 ? relativeFile : undefined, target.prefix || undefined);
      }

      const diffs: string[] = [];
      for (const repoConfig of repoConfigs) {
        const repoDiff = await gitDiff(repoConfig.dir, undefined, repoConfig.prefix || undefined);
        if (repoDiff) {
          diffs.push(repoDiff.trimEnd());
        }
      }

      return diffs.join("\n");
    },
  };
}

export function createTitleGenerator(
  generateTitleFn: (prompt: string) => Promise<string> = defaultGenerateTitle,
): TitleGenerator {
  return {
    generateTitle(prompt: string): Promise<string> {
      return generateTitleFn(prompt);
    },
  };
}

export function createSessionBootstrapper(
  initializeSessionReposFn: typeof defaultInitializeSessionRepos = defaultInitializeSessionRepos,
): SessionBootstrapper {
  return {
    initializeSessionRepos(sessionId: string, repos: string[], githubToken?: string) {
      return initializeSessionReposFn(sessionId, repos, githubToken);
    },
  };
}

export class ComposedNetclodeAgent implements NetclodeAgent {
  readonly capabilities: AgentCapabilities;

  private readonly titleGenerator?: TitleGenerator;
  private readonly gitInspector?: GitInspector;
  private readonly sessionBootstrapper?: SessionBootstrapper;

  constructor(
    private readonly backend: NetclodePromptBackend,
    dependencies: NetclodeAgentDependencies = {},
  ) {
    this.titleGenerator = dependencies.titleGenerator;
    this.gitInspector = dependencies.gitInspector;
    this.sessionBootstrapper = dependencies.sessionBootstrapper;
    this.capabilities = createAgentCapabilities({
      ...backend.capabilities,
      titleGeneration: Boolean(this.titleGenerator),
      gitStatus: Boolean(this.gitInspector),
      gitDiff: Boolean(this.gitInspector),
    });
  }

  async *executePrompt(sessionId: string, text: string, config?: PromptConfig) {
    if (config?.repos && config.repos.length > 0 && this.sessionBootstrapper) {
      for await (const event of this.sessionBootstrapper.initializeSessionRepos(sessionId, config.repos, config.githubToken)) {
        yield event;
      }
    }

    for await (const event of this.backend.executePrompt(sessionId, text, config)) {
      yield event;
    }
  }

  async interrupt(): Promise<void> {
    if (!this.capabilities.interrupt) {
      throw new UnsupportedAgentCapabilityError("interrupt");
    }

    await this.backend.interrupt();
  }

  async generateTitle(prompt: string): Promise<string> {
    if (!this.titleGenerator) {
      throw new UnsupportedAgentCapabilityError("titleGeneration");
    }

    return this.titleGenerator.generateTitle(prompt);
  }

  async getGitStatus(context?: RepositoryContext): Promise<AgentGitFileChange[]> {
    if (!this.gitInspector) {
      throw new UnsupportedAgentCapabilityError("gitStatus");
    }

    return this.gitInspector.getGitStatus(context);
  }

  async getGitDiff(context?: RepositoryContext, file?: string): Promise<string> {
    if (!this.gitInspector) {
      throw new UnsupportedAgentCapabilityError("gitDiff");
    }

    return this.gitInspector.getGitDiff(context, file);
  }

  async shutdown(): Promise<void> {
    await this.backend.shutdown();
  }
}
