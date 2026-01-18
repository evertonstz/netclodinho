#!/usr/bin/env npx ts-node

/**
 * Integration test for session auto-naming.
 * Connects to production, creates a session, sends a prompt,
 * and verifies the session name gets auto-generated.
 *
 * Usage: npx ts-node scripts/test-auto-naming.ts
 */

import WebSocket from "ws";

const WS_URL = "ws://netclode/ws";
const TIMEOUT_MS = 30000;

interface Session {
  id: string;
  name: string;
  status: string;
}

interface ServerMessage {
  type: string;
  session?: Session;
  sessions?: Session[];
  [key: string]: unknown;
}

async function testAutoNaming(): Promise<void> {
  console.log(`Connecting to ${WS_URL}...`);

  const ws = new WebSocket(WS_URL);
  let sessionId: string | null = null;
  let initialName: string | null = null;
  let finalName: string | null = null;
  let resolved = false;

  const cleanup = () => {
    if (sessionId) {
      console.log(`Cleaning up: deleting session ${sessionId}`);
      ws.send(JSON.stringify({ type: "session.delete", id: sessionId }));
    }
    setTimeout(() => ws.close(), 500);
  };

  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      if (!resolved) {
        resolved = true;
        console.error("❌ Timeout waiting for auto-naming");
        cleanup();
        reject(new Error("Timeout"));
      }
    }, TIMEOUT_MS);

    ws.on("open", () => {
      console.log("✓ Connected");

      // Sync first
      ws.send(JSON.stringify({ type: "sync" }));
    });

    ws.on("message", (data) => {
      const msg: ServerMessage = JSON.parse(data.toString());
      console.log(`← ${msg.type}`, msg.session?.name ? `(name: "${msg.session.name}")` : "");

      switch (msg.type) {
        case "sync.response":
          // Create a test session
          console.log("Creating test session...");
          ws.send(
            JSON.stringify({
              type: "session.create",
              name: null, // Let server assign default name
              initialPrompt: "What is 2+2? Reply in one word.",
            })
          );
          break;

        case "session.created":
          if (msg.session) {
            sessionId = msg.session.id;
            initialName = msg.session.name;
            console.log(`✓ Session created: ${sessionId}`);
            console.log(`  Initial name: "${initialName}"`);

            // Open the session to subscribe to updates
            ws.send(
              JSON.stringify({
                type: "session.open",
                id: sessionId,
              })
            );
          }
          break;

        case "session.updated":
          if (msg.session && msg.session.id === sessionId) {
            const newName = msg.session.name;
            console.log(`✓ Session updated: name="${newName}", status=${msg.session.status}`);

            // Check if name changed (auto-naming worked)
            if (newName !== initialName && newName !== "New Session") {
              finalName = newName;
              console.log("\n✅ AUTO-NAMING WORKS!");
              console.log(`   Initial: "${initialName}"`);
              console.log(`   Final:   "${finalName}"`);
              resolved = true;
              clearTimeout(timeout);
              cleanup();
              resolve();
            }
          }
          break;

        case "agent.done":
          // Agent finished but we might not have received the name update yet
          console.log("Agent done, waiting for session.updated with new name...");
          break;

        case "session.error":
        case "agent.error":
        case "error":
          console.error("❌ Error:", msg);
          resolved = true;
          clearTimeout(timeout);
          cleanup();
          reject(new Error(JSON.stringify(msg)));
          break;
      }
    });

    ws.on("error", (err) => {
      console.error("WebSocket error:", err.message);
      if (!resolved) {
        resolved = true;
        clearTimeout(timeout);
        reject(err);
      }
    });

    ws.on("close", () => {
      console.log("Connection closed");
      if (!resolved) {
        resolved = true;
        clearTimeout(timeout);
        reject(new Error("Connection closed unexpectedly"));
      }
    });
  });
}

// Run the test
testAutoNaming()
  .then(() => {
    console.log("\nTest passed!");
    process.exit(0);
  })
  .catch((err) => {
    console.error("\nTest failed:", err.message);
    process.exit(1);
  });
