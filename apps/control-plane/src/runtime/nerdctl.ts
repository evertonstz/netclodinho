/**
 * nerdctl/containerd runtime wrapper
 *
 * Manages Kata Containers VMs via nerdctl CLI
 */
import { $ } from "bun";
import { config } from "../config";

const RUNTIME = "io.containerd.kata-clh.v2";
const DEFAULT_IMAGE = config.agentImage || "ghcr.io/stanislas/netclode-agent:latest";

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
  image: string;
  createdAt: string;
  ipAddress?: string;
}

/**
 * Create and start a new VM for a session
 */
export async function createVM(vmConfig: VMConfig): Promise<string> {
  const {
    sessionId,
    cpus = 2,
    memoryMB = 2048,
    image = DEFAULT_IMAGE,
    env = {},
  } = vmConfig;

  const containerName = `sess-${sessionId}`;
  const workspacePath = `${config.juicefsRoot}/sessions/${sessionId}/workspace`;

  // Build environment flags
  const envFlags: string[] = [];
  const allEnv = {
    SESSION_ID: sessionId,
    WORKSPACE: "/workspace",
    ANTHROPIC_API_KEY: config.anthropicApiKey,
    ...env,
  };

  for (const [key, value] of Object.entries(allEnv)) {
    if (value) {
      envFlags.push("--env", `${key}=${value}`);
    }
  }

  // Create and start the VM
  const result = await $`nerdctl run -d \
    --runtime ${RUNTIME} \
    --name ${containerName} \
    --cpus ${cpus} \
    --memory ${memoryMB}m \
    --mount type=bind,src=${workspacePath},dst=/workspace \
    --label netclode.session=${sessionId} \
    --label netclode.created=${new Date().toISOString()} \
    ${envFlags} \
    ${image}`.text();

  return result.trim();
}

/**
 * Get VM's IP address
 */
export async function getVMIPAddress(sessionId: string): Promise<string | null> {
  try {
    const result = await $`nerdctl inspect sess-${sessionId} --format '{{.NetworkSettings.IPAddress}}'`.text();
    const ip = result.trim();
    return ip || null;
  } catch {
    return null;
  }
}

/**
 * List all session VMs
 */
export async function listVMs(): Promise<VMInfo[]> {
  try {
    const result = await $`nerdctl ps -a --filter label=netclode.session --format json`.text();

    if (!result.trim()) {
      return [];
    }

    // nerdctl outputs one JSON object per line
    return result
      .trim()
      .split("\n")
      .filter(Boolean)
      .map((line) => {
        const container = JSON.parse(line);
        return {
          id: container.ID,
          name: container.Names,
          status: container.Status,
          image: container.Image,
          createdAt: container.CreatedAt,
        };
      });
  } catch (error) {
    console.error("Failed to list VMs:", error);
    return [];
  }
}

/**
 * Get VM info by session ID
 */
export async function getVM(sessionId: string): Promise<VMInfo | null> {
  try {
    const result = await $`nerdctl inspect sess-${sessionId}`.json();
    if (!result || !result[0]) return null;

    const container = result[0];
    return {
      id: container.Id,
      name: container.Name,
      status: container.State.Status,
      image: container.Image,
      createdAt: container.Created,
      ipAddress: container.NetworkSettings?.IPAddress,
    };
  } catch {
    return null;
  }
}

/**
 * Check if VM is running
 */
export async function isVMRunning(sessionId: string): Promise<boolean> {
  const vm = await getVM(sessionId);
  return vm?.status === "running";
}

/**
 * Stop a VM
 */
export async function stopVM(sessionId: string): Promise<void> {
  await $`nerdctl stop sess-${sessionId}`.quiet();
}

/**
 * Remove a VM
 */
export async function removeVM(sessionId: string): Promise<void> {
  await $`nerdctl rm -f sess-${sessionId}`.quiet();
}

/**
 * Execute a command in a VM (for debugging/admin only)
 */
export async function execInVM(
  sessionId: string,
  command: string[],
  options?: { tty?: boolean }
): Promise<{ stdout: string; stderr: string; exitCode: number }> {
  const flags = options?.tty ? ["-it"] : [];

  const proc = Bun.spawn(
    ["nerdctl", "exec", ...flags, `sess-${sessionId}`, ...command],
    {
      stdout: "pipe",
      stderr: "pipe",
    }
  );

  const [stdout, stderr] = await Promise.all([
    new Response(proc.stdout).text(),
    new Response(proc.stderr).text(),
  ]);

  return {
    stdout,
    stderr,
    exitCode: await proc.exited,
  };
}

/**
 * Get VM logs
 */
export async function getVMLogs(
  sessionId: string,
  options?: { follow?: boolean; tail?: number }
): Promise<string> {
  const flags: string[] = [];
  if (options?.tail) flags.push("--tail", String(options.tail));

  const result = await $`nerdctl logs ${flags} sess-${sessionId}`.text();
  return result;
}

/**
 * Pull the agent image
 */
export async function pullImage(image: string = DEFAULT_IMAGE): Promise<void> {
  console.log(`Pulling image: ${image}`);
  await $`nerdctl pull ${image}`;
}

/**
 * Wait for VM to be ready (agent HTTP API responding)
 */
export async function waitForVMReady(
  sessionId: string,
  timeoutMs: number = 60000
): Promise<string | null> {
  const startTime = Date.now();
  const checkInterval = 1000;

  while (Date.now() - startTime < timeoutMs) {
    try {
      // Get VM IP
      const ip = await getVMIPAddress(sessionId);
      if (ip) {
        // Try to reach the agent health endpoint
        const response = await fetch(`http://${ip}:3002/health`, {
          signal: AbortSignal.timeout(2000),
        });
        if (response.ok) {
          return ip;
        }
      }
    } catch {
      // Not ready yet
    }

    await new Promise((resolve) => setTimeout(resolve, checkInterval));
  }

  return null;
}
