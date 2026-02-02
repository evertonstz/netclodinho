#!/usr/bin/env node
import { connectToControlPlane } from "./connect-client.js";

const controlPlaneUrl = process.env.CONTROL_PLANE_URL || "http://control-plane.netclode.svc.cluster.local";
const podName = process.env.POD_NAME || process.env.HOSTNAME;

// Session ID can be provided directly (direct mode) or discovered via warm pool
const sessionId = process.env.SESSION_ID;

console.log("[agent] Starting agent...");
console.log(`[agent] Config: controlPlaneUrl=${controlPlaneUrl}, podName=${podName}, sessionId=${sessionId || "(warm pool mode)"}`);
console.log(`[agent] Environment: ANTHROPIC_API_KEY=${process.env.ANTHROPIC_API_KEY ? "set" : "NOT SET"}`);

// Connect to control plane
async function main() {
  try {
    // Connect immediately - in warm pool mode, session will be pushed via gRPC
    await connectToControlPlane(controlPlaneUrl, sessionId, podName);
  } catch (error) {
    console.error("[agent] Error:", error);
    // Restart after error
    setTimeout(main, 5000);
  }
}

main();
