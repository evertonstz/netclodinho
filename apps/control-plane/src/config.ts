export const config = {
  // Server
  port: Number(process.env.PORT) || 3000,

  // Anthropic
  anthropicApiKey: process.env.ANTHROPIC_API_KEY || "",

  // Storage
  juicefsRoot: process.env.JUICEFS_ROOT || "/juicefs",

  // Runtime
  agentImage: process.env.AGENT_IMAGE || "ghcr.io/stanislas/netclode-agent:latest",

  // VM defaults
  defaultCpus: Number(process.env.DEFAULT_CPUS) || 2,
  defaultMemoryMB: Number(process.env.DEFAULT_MEMORY_MB) || 2048,
};
