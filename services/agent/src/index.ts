#!/usr/bin/env node
import { connectToControlPlane } from "./connect-client.js";

const controlPlaneUrl = process.env.CONTROL_PLANE_URL || "http://control-plane.netclode.svc.cluster.local";
const podName = process.env.POD_NAME || process.env.HOSTNAME;

// Session ID can be provided directly (direct mode) or discovered via warm pool
let sessionId = process.env.SESSION_ID;

console.log("[agent] Starting agent...");
console.log(`[agent] Config: controlPlaneUrl=${controlPlaneUrl}, podName=${podName}, sessionId=${sessionId || "(warm pool mode)"}`);
console.log(`[agent] Environment: ANTHROPIC_API_KEY=${process.env.ANTHROPIC_API_KEY ? "set" : "NOT SET"}`);

/**
 * Fetch session config from control plane (for warm pool mode)
 */
async function fetchSessionConfig(): Promise<{ sessionId: string } | null> {
  if (!podName) {
    return null;
  }

  try {
    const response = await fetch(`${controlPlaneUrl}/internal/session-config?pod=${podName}`);
    if (response.ok) {
      const config = await response.json() as Record<string, string>;
      if (config.SESSION_ID) {
        return { sessionId: config.SESSION_ID };
      }
    }
  } catch (error) {
    // Expected when not yet bound to a session
  }
  return null;
}

/**
 * Wait for session assignment (warm pool mode)
 */
async function waitForSession(): Promise<string> {
  console.log("[agent] Waiting for session assignment...");
  
  while (true) {
    const config = await fetchSessionConfig();
    if (config) {
      console.log(`[agent] Session assigned: ${config.sessionId}`);
      return config.sessionId;
    }
    
    // Poll every 2 seconds
    await new Promise(resolve => setTimeout(resolve, 2000));
  }
}

// Connect to control plane
async function main() {
  try {
    // If no session ID provided, wait for assignment (warm pool mode)
    if (!sessionId) {
      sessionId = await waitForSession();
    }

    await connectToControlPlane(controlPlaneUrl, sessionId);
  } catch (error) {
    console.error("[agent] Error:", error);
    // Restart after error
    setTimeout(main, 5000);
  }
}

main();
