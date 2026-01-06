export const config = {
  // Server
  port: Number(process.env.PORT) || 3000,

  // Anthropic
  anthropicApiKey: process.env.ANTHROPIC_API_KEY || "",

  // Kubernetes
  k8sNamespace: process.env.K8S_NAMESPACE || "netclode",

  // Runtime
  agentImage: process.env.AGENT_IMAGE || "ghcr.io/stanislas/netclode-agent:latest",
  sandboxTemplate: process.env.SANDBOX_TEMPLATE || "netclode-agent",

  // VM defaults
  defaultCpus: Number(process.env.DEFAULT_CPUS) || 2,
  defaultMemoryMB: Number(process.env.DEFAULT_MEMORY_MB) || 2048,

  // Redis Sessions
  redisUrl:
    process.env.REDIS_URL ||
    "redis://redis-sessions.netclode.svc.cluster.local:6379",
  maxMessagesPerSession: Number(process.env.MAX_MESSAGES_PER_SESSION) || 1000,
  maxEventsPerSession: Number(process.env.MAX_EVENTS_PER_SESSION) || 50,
};
