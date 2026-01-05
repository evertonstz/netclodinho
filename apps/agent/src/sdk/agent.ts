import { query, type SDKMessage } from "@anthropic-ai/claude-agent-sdk";
import { config } from "../config";

export type EventSender = (data: object) => Promise<void>;

export interface AgentInstance {
  run(prompt: string, send: EventSender): Promise<void>;
  interrupt(): void;
}

let currentQuery: ReturnType<typeof query> | null = null;

export function createAgent(): AgentInstance {
  return {
    async run(prompt: string, send: EventSender) {
      try {
        currentQuery = query({
          prompt,
          options: {
            tools: { type: "preset", preset: "claude_code" },
            allowedTools: ["Read", "Write", "Edit", "Bash", "Glob", "Grep"],
            permissionMode: "bypassPermissions",
            allowDangerouslySkipPermissions: true,
            cwd: config.workspacePath,
            model: "claude-sonnet-4-20250514",
            persistSession: false,
          },
        });

        for await (const message of currentQuery) {
          await handleMessage(message, send);
        }
      } catch (error) {
        if (error instanceof Error && error.message.includes("abort")) {
          await send({ type: "agent.interrupted" });
        } else {
          console.error("[agent] Error:", error);
          await send({
            type: "agent.error",
            error: error instanceof Error ? error.message : String(error),
          });
        }
      } finally {
        currentQuery = null;
      }
    },

    interrupt() {
      currentQuery?.interrupt();
    },
  };
}

async function handleMessage(message: SDKMessage, send: EventSender) {
  switch (message.type) {
    case "system":
      await send({ type: "agent.system", subtype: message.subtype });
      break;

    case "assistant":
      if (message.message?.content) {
        for (const block of message.message.content) {
          if (block.type === "text") {
            await send({ type: "agent.message", content: block.text, partial: false });
          } else if (block.type === "tool_use") {
            await send({
              type: "agent.event",
              event: {
                kind: "tool_start",
                tool: block.name,
                toolUseId: block.id,
                input: block.input,
                timestamp: new Date().toISOString(),
              },
            });
          }
        }
      }
      break;

    case "user":
      if (message.message?.content && Array.isArray(message.message.content)) {
        for (const block of message.message.content) {
          if (typeof block === "object" && block.type === "tool_result") {
            await send({
              type: "agent.event",
              event: {
                kind: "tool_end",
                toolUseId: block.tool_use_id,
                timestamp: new Date().toISOString(),
              },
            });
          }
        }
      }
      break;

    case "result":
      if (message.subtype === "success") {
        await send({
          type: "agent.result",
          result: message.result,
          numTurns: message.num_turns,
          costUsd: message.total_cost_usd,
        });
      }
      break;

    case "stream_event":
      if (message.event.type === "content_block_delta") {
        const delta = message.event.delta;
        if (delta && "text" in delta) {
          await send({ type: "agent.message", content: delta.text, partial: true });
        }
      }
      break;
  }
}
