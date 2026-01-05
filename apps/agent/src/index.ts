#!/usr/bin/env bun
import { createServer } from "http";
import { query } from "@anthropic-ai/claude-agent-sdk";

const port = parseInt(process.env.AGENT_PORT || "3002", 10);
const workspace = process.env.WORKSPACE || "/workspace";

const server = createServer(async (req, res) => {
  const url = new URL(req.url || "/", `http://localhost:${port}`);

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
    const { text } = JSON.parse(body) as { text: string };
    console.error(`[prompt] Received: ${text.slice(0, 50)}...`);

    res.writeHead(200, {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache",
      Connection: "keep-alive",
    });

    const send = (data: object) => {
      res.write(`data: ${JSON.stringify(data)}\n\n`);
    };

    try {
      send({ type: "start" });

      const q = query({
        prompt: text,
        options: {
          cwd: workspace,
          dangerouslySkipPermissions: true,
          model: "claude-opus-4-5-20251101",
          executable: "/nix/var/nix/profiles/default/bin/node",
        },
      });

      for await (const message of q) {
        switch (message.type) {
          case "system":
            send({ type: "agent.system", subtype: message.subtype });
            break;
          case "assistant":
            if (message.message?.content) {
              for (const block of message.message.content) {
                if (block.type === "text") {
                  send({ type: "agent.message", content: block.text, partial: false });
                } else if (block.type === "tool_use") {
                  send({
                    type: "agent.event",
                    event: { kind: "tool_start", tool: block.name, toolUseId: block.id, input: block.input },
                  });
                }
              }
            }
            break;
          case "user":
            if (message.message?.content && Array.isArray(message.message.content)) {
              for (const block of message.message.content) {
                if (typeof block === "object" && block.type === "tool_result") {
                  send({ type: "agent.event", event: { kind: "tool_end", toolUseId: block.tool_use_id } });
                }
              }
            }
            break;
          case "result":
            if (message.subtype === "success") {
              send({ type: "agent.result", result: message.result, numTurns: message.num_turns, costUsd: message.total_cost_usd });
            }
            break;
          case "stream_event":
            if (message.event.type === "content_block_delta") {
              const delta = message.event.delta;
              if (delta && "text" in delta) {
                send({ type: "agent.message", content: delta.text, partial: true });
              }
            }
            break;
        }
      }

      send({ type: "done" });
    } catch (error) {
      console.error("[prompt] Error:", error);
      send({ type: "error", error: String(error) });
    } finally {
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
