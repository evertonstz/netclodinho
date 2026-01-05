import { config } from "./config";
import { query } from "@anthropic-ai/claude-agent-sdk";

const cwd = config.workspacePath || "/workspace";

async function* streamPrompt(text: string) {
  const encoder = new TextEncoder();
  const send = (data: object) => encoder.encode(`data: ${JSON.stringify(data)}\n\n`);

  yield send({ type: "start" });

  for await (const message of query({
    prompt: text,
    options: {
      cwd,
      dangerouslySkipPermissions: true,
      model: "claude-opus-4-5-20251101",
    },
  })) {
    switch (message.type) {
      case "system":
        yield send({ type: "agent.system", subtype: message.subtype });
        break;
      case "assistant":
        if (message.message?.content) {
          for (const block of message.message.content) {
            if (block.type === "text") {
              yield send({ type: "agent.message", content: block.text, partial: false });
            } else if (block.type === "tool_use") {
              yield send({ type: "agent.event", event: { kind: "tool_start", tool: block.name, toolUseId: block.id, input: block.input } });
            }
          }
        }
        break;
      case "user":
        if (message.message?.content && Array.isArray(message.message.content)) {
          for (const block of message.message.content) {
            if (typeof block === "object" && block.type === "tool_result") {
              yield send({ type: "agent.event", event: { kind: "tool_end", toolUseId: block.tool_use_id } });
            }
          }
        }
        break;
      case "result":
        if (message.subtype === "success") {
          yield send({ type: "agent.result", result: message.result, numTurns: message.num_turns, costUsd: message.total_cost_usd });
        }
        break;
    }
  }

  yield send({ type: "done" });
}

const server = Bun.serve({
  port: config.port || 3002,
  async fetch(req) {
    const url = new URL(req.url);

    if (url.pathname === "/health") {
      return new Response("ok");
    }

    if (url.pathname === "/prompt" && req.method === "POST") {
      const { text } = (await req.json()) as { text: string };
      console.error(`[prompt] Received: ${text.slice(0, 50)}...`);

      const iterator = streamPrompt(text);
      const stream = new ReadableStream({
        async pull(controller) {
          const { value, done } = await iterator.next();
          if (done) {
            controller.close();
          } else {
            controller.enqueue(value);
          }
        },
      });

      return new Response(stream, {
        headers: {
          "Content-Type": "text/event-stream",
          "Cache-Control": "no-cache",
          Connection: "keep-alive",
        },
      });
    }

    if (url.pathname === "/interrupt" && req.method === "POST") {
      return new Response(JSON.stringify({ ok: true }));
    }

    return new Response("Not found", { status: 404 });
  },
});

console.log(`Agent server listening on http://localhost:${server.port}`);
