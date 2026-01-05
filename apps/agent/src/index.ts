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

      // Use ReadableStream with pull-based approach
      let done = false;
      const queue: string[] = [];
      let resolve: (() => void) | null = null;

      const send = async (data: object) => {
        const chunk = `data: ${JSON.stringify(data)}\n\n`;
        console.error(`[prompt] Sending: ${chunk.slice(0, 80)}...`);
        queue.push(chunk);
        if (resolve) {
          resolve();
          resolve = null;
        }
      };

      // Start agent processing
      (async () => {
        try {
          await send({ type: "start" });
          await agent.run(text, send);
          await send({ type: "done" });
        } catch (error) {
          console.error(`[prompt] Error:`, error);
          await send({ type: "error", error: String(error) });
        } finally {
          done = true;
          if (resolve) resolve();
        }
      })();

      const encoder = new TextEncoder();
      const stream = new ReadableStream({
        async pull(controller) {
          while (queue.length === 0 && !done) {
            await new Promise<void>((r) => (resolve = r));
          }
          while (queue.length > 0) {
            controller.enqueue(encoder.encode(queue.shift()!));
          }
          if (done && queue.length === 0) {
            controller.close();
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

    // Interrupt
    if (url.pathname === "/interrupt" && req.method === "POST") {
      agent.interrupt();
      return new Response(JSON.stringify({ ok: true }));
    }

    return new Response("Not found", { status: 404 });
  },
});

console.log(`Agent server listening on http://localhost:${server.port}`);
