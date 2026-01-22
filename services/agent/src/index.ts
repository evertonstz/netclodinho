#!/usr/bin/env node
import { startConnectServer } from "./connect-server.js";

const agentPort = parseInt(process.env.AGENT_PORT || "3002", 10);

console.log("[agent] Starting agent server...");
console.log(`[agent] Config: agentPort=${agentPort}`);
console.log(`[agent] Environment: ANTHROPIC_API_KEY=${process.env.ANTHROPIC_API_KEY ? "set" : "NOT SET"}`);

// Start Connect server for gRPC/Connect protocol
startConnectServer();
