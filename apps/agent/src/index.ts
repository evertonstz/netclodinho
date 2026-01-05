import { createAgent } from "./sdk/agent";
import { config } from "./config";

const agent = createAgent();

const server = Bun.serve({
  port: config.port || 3002,
  async fetch(req) {
    const url = new URL(req.url);

    // Health check
    if (url.pathname === "/health") {
      return new Response("ok");
    }

    // Handle prompt requests
    if (url.pathname === "/prompt" && req.method === "POST") {
      const body = (await req.json()) as { sessionId: string; text: string };
      const { text } = body;
      console.error(`[prompt] Received: ${text.slice(0, 50)}...`);

      // Use TransformStream for better streaming support
      const { readable, writable } = new TransformStream();
      const writer = writable.getWriter();
      const encoder = new TextEncoder();

      const send = async (data: object) => {
        const chunk = `data: ${JSON.stringify(data)}\n\n`;
        await writer.write(encoder.encode(chunk));
      };

      // Run agent in background, streaming events
      (async () => {
        try {
          await agent.run(text, send);
          await send({ type: "done" });
        } catch (error) {
          console.error(`[prompt] Error:`, error);
          await send({ type: "error", error: String(error) });
        } finally {
          await writer.close();
        }
      })();

      return new Response(readable, {
        headers: {
          "Content-Type": "text/event-stream",
          "Cache-Control": "no-cache",
          Connection: "keep-alive",
        },
      });
    }

    // Interrupt
    if (url.pathname === "/interrupt" && req.method === "POST") {
      agent.interrupt();
      return new Response(JSON.stringify({ ok: true }));
    }

    return new Response("Not found", { status: 404 });
  },
});

console.log(`Agent server listening on http://localhost:${server.port}`);
