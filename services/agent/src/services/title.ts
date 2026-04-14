/**
 * Title generation service - generates session titles using Claude Haiku
 */

import Anthropic from "@anthropic-ai/sdk";
import type { ClientOptions } from "@anthropic-ai/sdk/client";
import { ProxyAgent, fetch as undiciFetch } from "undici";

/**
 * Generate a short title for a session based on the initial prompt
 */
const proxyUrl = process.env.HTTPS_PROXY || process.env.HTTP_PROXY;
const proxyDispatcher = proxyUrl ? new ProxyAgent(proxyUrl) : undefined;
// Use a proxy-aware fetch so BoxLite/K8s networking can apply the configured secret-handling path.
const proxyFetch: ClientOptions["fetch"] = proxyDispatcher
  ? (input, init) =>
      undiciFetch(input, { ...(init ?? {}), dispatcher: proxyDispatcher })
  : undefined;

export async function generateTitle(prompt: string): Promise<string> {
  console.log(`[title] Title generation requested for: "${prompt.slice(0, 50)}..."`);

  const anthropic = new Anthropic({ fetch: proxyFetch });
  const response = await anthropic.messages.create({
    model: "claude-haiku-4-5",
    max_tokens: 30,
    messages: [
      {
        role: "user",
        content: `Generate a 3-5 word title for this task. Be specific and concise.\n\nTask: "${prompt.slice(0, 300)}"\n\nReply with only the title.`,
      },
    ],
  });

  const title = (response.content[0] as { type: "text"; text: string }).text.trim();
  console.log(`[title] Generated title: "${title}"`);
  return title;
}
