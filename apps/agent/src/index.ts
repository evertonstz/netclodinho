#!/usr/bin/env node
import { createServer } from "http";
import { query } from "@anthropic-ai/claude-agent-sdk";

const port = parseInt(process.env.AGENT_PORT || "3002", 10);
const workspace = process.env.WORKSPACE || "/workspace";
const gitRepo = process.env.GIT_REPO;

function buildSystemPrompt(): { type: "preset"; preset: "claude_code"; append: string } {
  const lines = [
    "## Environment",
    "",
    "You are running inside an isolated sandbox (Kata Container microVM).",
    `- Working directory: ${workspace}`,
    "- This directory is persistent across sessions",
    "- You have full shell, network, and Docker access",
    "- It is safe to run any commands - the sandbox is isolated",
    "",
    "## Tools",
    "",
    "- **mise** is installed for managing tool versions (Node, Python, Go, Rust, etc.)",
    "  - Use `mise use node@22` to install and activate Node.js 22",
    "  - Use `mise use python@3.12` for Python",
    "  - Use `mise use go@latest` for Go",
    "  - See `mise --help` for more options",
  ];

  if (gitRepo) {
    lines.push("", "## Repository", "", `The repository ${gitRepo} has been cloned to ${workspace}.`);
  }

  return {
    type: "preset",
    preset: "claude_code",
    append: lines.join("\n"),
  };
}

console.log("[agent] Starting agent server...");
console.log(`[agent] Config: port=${port}, workspace=${workspace}`);
console.log(`[agent] Environment: ANTHROPIC_API_KEY=${process.env.ANTHROPIC_API_KEY ? "set" : "NOT SET"}`);

// Map control plane session IDs to SDK session IDs
const sessionMap = new Map<string, string>();

const server = createServer(async (req, res) => {
  const url = new URL(req.url || "/", `http://localhost:${port}`);
  console.log(`[agent] ${req.method} ${url.pathname}`);

  if (url.pathname === "/health") {
    res.writeHead(200);
    res.end("ok");
    return;
  }

  if (url.pathname === "/prompt" && req.method === "POST") {
    let body = "";
    for await (const chunk of req) {
      body += chunk;
    }
    const { text, sessionId } = JSON.parse(body) as { text: string; sessionId?: string };
    console.log(`[agent] Prompt received (session=${sessionId}): "${text.slice(0, 100)}${text.length > 100 ? "..." : ""}"`);

    res.writeHead(200, {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache",
      Connection: "keep-alive",
    });

    const send = (data: object) => {
      const json = JSON.stringify(data);
      console.log(`[agent] SSE send: ${json.slice(0, 150)}${json.length > 150 ? "..." : ""}`);
      res.write(`data: ${json}\n\n`);
    };

    try {
      send({ type: "start" });

      // Look up the SDK session ID from our mapping
      const sdkSessionId = sessionId ? sessionMap.get(sessionId) : undefined;
      console.log(`[agent] SDK session lookup: ${sessionId} -> ${sdkSessionId || "(new session)"}`);
      console.log(`[agent] Current session mappings: ${JSON.stringify(Object.fromEntries(sessionMap))}`);

      console.log(`[agent] Calling Claude Agent SDK query()...`);
      const q = query({
        prompt: text,
        options: {
          cwd: workspace,
          permissionMode: "bypassPermissions",
          allowDangerouslySkipPermissions: true,
          model: "claude-opus-4-5-20251101",
          persistSession: true,
          systemPrompt: buildSystemPrompt(),
          ...(sdkSessionId && { resume: sdkSessionId }),
        },
      });
      console.log(`[agent] SDK query created, starting iteration...`);

      let messageCount = 0;
      for await (const message of q) {
        messageCount++;
        console.log(`[agent] SDK message #${messageCount}: type=${message.type}${message.type === "system" ? `, subtype=${(message as { subtype?: string }).subtype}` : ""}`);
        switch (message.type) {
          case "system":
            // Capture the SDK session ID from the init message
            if (message.subtype === "init" && sessionId && message.session_id) {
              const existingMapping = sessionMap.get(sessionId);
              if (!existingMapping) {
                sessionMap.set(sessionId, message.session_id);
                console.log(`[agent] Stored session mapping: ${sessionId} -> ${message.session_id}`);
              }
            }
            send({ type: "agent.system", subtype: message.subtype });
            break;
          case "assistant":
            if (message.message?.content) {
              console.log(`[agent] Assistant message with ${message.message.content.length} blocks`);
              for (const block of message.message.content) {
                if (block.type === "text") {
                  console.log(`[agent] Text block: ${block.text.slice(0, 100)}...`);
                  send({ type: "agent.message", content: block.text, partial: false });
                } else if (block.type === "tool_use") {
                  console.log(`[agent] Tool use: ${block.name}`);
                  send({
                    type: "agent.event",
                    event: { kind: "tool_start", tool: block.name, toolUseId: block.id, input: block.input, timestamp: new Date().toISOString() },
                  });
                }
              }
            }
            break;
          case "user":
            if (message.message?.content && Array.isArray(message.message.content)) {
              for (const block of message.message.content) {
                if (typeof block === "object" && block.type === "tool_result") {
                  console.log(`[agent] Tool result: ${block.tool_use_id}`);
                  const isError = block.is_error === true;
                  send({
                    type: "agent.event",
                    event: {
                      kind: "tool_end",
                      tool: "unknown", // SDK doesn't provide tool name in result
                      toolUseId: block.tool_use_id,
                      result: typeof block.content === "string" ? block.content : undefined,
                      error: isError ? (typeof block.content === "string" ? block.content : "Tool error") : undefined,
                      timestamp: new Date().toISOString(),
                    },
                  });
                }
              }
            }
            break;
          case "result":
            console.log(`[agent] Result: subtype=${message.subtype}`);
            if (message.subtype === "success") {
              send({ type: "agent.result", result: message.result, numTurns: message.num_turns, costUsd: message.total_cost_usd });
            }
            break;
          case "stream_event":
            if (message.event.type === "content_block_delta") {
              const delta = message.event.delta;
              if (delta && "text" in delta) {
                // Don't log every streaming delta, too noisy
                send({ type: "agent.message", content: delta.text, partial: true });
              }
            }
            break;
        }
      }

      console.log(`[agent] SDK iteration complete, received ${messageCount} messages`);
      send({ type: "done" });
    } catch (error) {
      console.error("[agent] Error during prompt:", error);
      send({ type: "error", error: String(error) });
    } finally {
      console.log(`[agent] Response ended`);
      res.end();
    }
    return;
  }

  if (url.pathname === "/interrupt" && req.method === "POST") {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ ok: true }));
    return;
  }

  res.writeHead(404);
  res.end("Not found");
});

server.listen(port, () => {
  console.log(`Agent server listening on http://localhost:${port}`);
});
