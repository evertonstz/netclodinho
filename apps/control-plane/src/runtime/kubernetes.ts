/**
 * Kubernetes runtime using agent-sandbox CRDs
 *
 * Manages agent sandboxes via SandboxClaim resources
 */
import * as k8s from "@kubernetes/client-node";
import { config } from "../config";

// Use getter to ensure env var is read at runtime, not bundle time
function getNamespace(): string {
  const ns = process.env.K8S_NAMESPACE || "netclode";
  return ns;
}

const SANDBOX_TEMPLATE = "netclode-agent";
const STORAGE_CLASS = "juicefs-sc";

export interface VMConfig {
  sessionId: string;
  cpus?: number;
  memoryMB?: number;
  image?: string;
  env?: Record<string, string>;
}

export interface VMInfo {
  id: string;
  name: string;
  status: string;
  serviceFQDN?: string;
}

interface SandboxClaimSpec {
  sandboxTemplateRef: {
    name: string;
  };
}

interface SandboxClaimStatus {
  conditions?: k8s.V1Condition[];
  sandboxRef?: {
    name: string;
  };
}

interface SandboxStatus {
  serviceFQDN?: string;
  conditions?: k8s.V1Condition[];
}

export class KubernetesRuntime {
  private kc: k8s.KubeConfig;
  private customApi: k8s.CustomObjectsApi;
  private coreApi: k8s.CoreV1Api;

  constructor() {
    this.kc = new k8s.KubeConfig();

    // Load config from default locations (in-cluster or KUBECONFIG env)
    if (process.env.KUBERNETES_SERVICE_HOST) {
      this.kc.loadFromCluster();
    } else {
      this.kc.loadFromDefault();
    }

    this.customApi = this.kc.makeApiClient(k8s.CustomObjectsApi);
    this.coreApi = this.kc.makeApiClient(k8s.CoreV1Api);
  }

  /**
   * Create a new sandbox for a session
   */
  async createSandbox(vmConfig: VMConfig): Promise<string> {
    const { sessionId, env = {} } = vmConfig;
    const name = `sess-${sessionId}`;

    // Create secret for environment variables
    await this.createEnvSecret(sessionId, {
      SESSION_ID: sessionId,
      ANTHROPIC_API_KEY: config.anthropicApiKey,
      ...env,
    });

    // Create Sandbox with volumeClaimTemplates (controller creates PVCs)
    const sandbox = {
      apiVersion: "agents.x-k8s.io/v1alpha1",
      kind: "Sandbox",
      metadata: {
        name,
        namespace: getNamespace(),
        labels: {
          "netclode.io/session": sessionId,
        },
      },
      spec: {
        podTemplate: {
          spec: {
            runtimeClassName: "kata-clh",
            containers: [
              {
                name: "agent",
                image: "ghcr.io/angristan/netclode-agent:latest",
                ports: [{ containerPort: 3002, name: "http" }],
                env: [
                  { name: "NODE_ENV", value: "production" },
                  { name: "WORKSPACE", value: "/workspace" },
                ],
                envFrom: [
                  {
                    secretRef: {
                      name: `${name}-env`,
                    },
                  },
                ],
                volumeMounts: [
                  { name: "workspace", mountPath: "/workspace" },
                ],
                resources: {
                  requests: { cpu: "100m", memory: "256Mi" },
                  limits: { cpu: "2", memory: "4Gi" },
                },
                readinessProbe: {
                  httpGet: { path: "/health", port: 3002 },
                  initialDelaySeconds: 10,
                  periodSeconds: 5,
                },
              },
            ],
          },
        },
        // Controller will create PVC from this template
        volumeClaimTemplates: [
          {
            metadata: {
              name: "workspace",
              labels: {
                "netclode.io/session": sessionId,
              },
            },
            spec: {
              accessModes: ["ReadWriteOnce"],
              storageClassName: STORAGE_CLASS,
              resources: {
                requests: {
                  storage: "10Gi",
                },
              },
            },
          },
        ],
      },
    };

    await this.customApi.createNamespacedCustomObject({
      group: "agents.x-k8s.io",
      version: "v1alpha1",
      namespace: getNamespace(),
      plural: "sandboxes",
      body: sandbox,
    });

    console.log(`[${sessionId}] Sandbox created: ${name}`);
    return name;
  }

  /**
   * Get sandbox status by session ID
   */
  async getSandboxStatus(sessionId: string): Promise<VMInfo | null> {
    const name = `sess-${sessionId}`;

    try {
      const sandbox = (await this.customApi.getNamespacedCustomObject({
        group: "agents.x-k8s.io",
        version: "v1alpha1",
        namespace: getNamespace(),
        plural: "sandboxes",
        name,
      })) as {
        metadata: k8s.V1ObjectMeta;
        status?: SandboxStatus;
      };

      return {
        id: name,
        name,
        status: this.mapConditionsToStatus(sandbox.status?.conditions),
        serviceFQDN: sandbox.status?.serviceFQDN,
      };
    } catch (e: unknown) {
      const error = e as { response?: { statusCode?: number } };
      if (error.response?.statusCode === 404) {
        return null;
      }
      throw e;
    }
  }

  /**
   * Delete a sandbox by session ID
   */
  async deleteSandbox(sessionId: string): Promise<void> {
    const name = `sess-${sessionId}`;

    // Delete Sandbox
    try {
      await this.customApi.deleteNamespacedCustomObject({
        group: "agents.x-k8s.io",
        version: "v1alpha1",
        namespace: getNamespace(),
        plural: "sandboxes",
        name,
      });
    } catch (e: unknown) {
      const error = e as { response?: { statusCode?: number } };
      if (error.response?.statusCode !== 404) {
        throw e;
      }
    }

    // Delete secret
    try {
      await this.coreApi.deleteNamespacedSecret({
        name: `sess-${sessionId}-env`,
        namespace: getNamespace(),
      });
    } catch {
      // Ignore errors
    }

    // Delete PVC (volumeClaimTemplate naming: {volumeName}-{sandboxName})
    try {
      await this.coreApi.deleteNamespacedPersistentVolumeClaim({
        name: `workspace-sess-${sessionId}`,
        namespace: getNamespace(),
      });
    } catch {
      // Ignore errors
    }

    console.log(`[${sessionId}] Sandbox deleted`);
  }

  /**
   * Wait for sandbox to be ready and return service FQDN
   */
  async waitForReady(sessionId: string, timeoutMs = 120000): Promise<string | null> {
    const startTime = Date.now();
    const checkInterval = 2000;

    while (Date.now() - startTime < timeoutMs) {
      const info = await this.getSandboxStatus(sessionId);

      if (info?.status === "ready" && info.serviceFQDN) {
        // Try to reach the agent health endpoint
        try {
          const response = await fetch(`http://${info.serviceFQDN}:3002/health`, {
            signal: AbortSignal.timeout(2000),
          });
          if (response.ok) {
            console.log(`[${sessionId}] Agent ready at ${info.serviceFQDN}`);
            return info.serviceFQDN;
          }
        } catch {
          // Not ready yet
        }
      }

      await new Promise((resolve) => setTimeout(resolve, checkInterval));
    }

    console.error(`[${sessionId}] Timeout waiting for agent to be ready`);
    return null;
  }

  /**
   * List all sandboxes
   */
  async listSandboxes(): Promise<VMInfo[]> {
    const list = (await this.customApi.listNamespacedCustomObject({
      group: "agents.x-k8s.io",
      version: "v1alpha1",
      namespace: getNamespace(),
      plural: "sandboxes",
      labelSelector: "netclode.io/session",
    })) as {
      items: Array<{
        metadata: k8s.V1ObjectMeta;
        status?: SandboxStatus;
      }>;
    };

    return list.items.map((item) => ({
      id: item.metadata.name || "",
      name: item.metadata.name || "",
      status: this.mapConditionsToStatus(item.status?.conditions),
    }));
  }

  /**
   * Check if sandbox is running
   */
  async isSandboxRunning(sessionId: string): Promise<boolean> {
    const info = await this.getSandboxStatus(sessionId);
    return info?.status === "ready";
  }

  private async createEnvSecret(
    sessionId: string,
    env: Record<string, string>
  ): Promise<void> {
    const namespace = getNamespace();
    const secret: k8s.V1Secret = {
      apiVersion: "v1",
      kind: "Secret",
      metadata: {
        name: `sess-${sessionId}-env`,
        namespace,
        labels: {
          "netclode.io/session": sessionId,
        },
      },
      type: "Opaque",
      stringData: env,
    };

    await this.coreApi.createNamespacedSecret({
      namespace,
      body: secret,
    });
    console.log(`[${sessionId}] Secret created`);
  }

  private mapConditionsToStatus(conditions?: k8s.V1Condition[]): string {
    if (!conditions?.length) return "pending";

    const ready = conditions.find((c) => c.type === "Ready");
    if (ready?.status === "True") return "ready";
    if (ready?.status === "False") return "error";

    return "creating";
  }
}

// Export singleton instance
export const kubernetesRuntime = new KubernetesRuntime();
