#!/usr/bin/env bun
import type { ClientMessage, ServerMessage } from "@netclode/protocol";
import * as readline from "readline";

const args = process.argv.slice(2);
let url = "ws://localhost:3000/ws";

// Parse args
for (let i = 0; i < args.length; i++) {
  if (args[i] === "--url" || args[i] === "-u") {
    url = args[++i];
  } else if (args[i] === "--help" || args[i] === "-h") {
    console.log(`
Netclode CLI - Debug client for Netclode control plane

Usage: netclode [options]

Options:
  -u, --url <url>   Control plane WebSocket URL (default: ws://localhost:3000/ws)
  -h, --help        Show this help

Commands (in interactive mode):
  create [name]     Create a new session
  list              List all sessions
  resume <id>       Resume a session
  pause <id>        Pause a session
  delete <id>       Delete a session
  prompt <id> <text> Send a prompt to a session
  interrupt <id>    Interrupt current prompt
  quit              Exit the CLI
`);
    process.exit(0);
  }
}

// Convert http(s) to ws(s) if needed
if (url.startsWith("http://")) {
  url = url.replace("http://", "ws://");
  if (!url.endsWith("/ws")) url += "/ws";
} else if (url.startsWith("https://")) {
  url = url.replace("https://", "wss://");
  if (!url.endsWith("/ws")) url += "/ws";
}

console.log(`Connecting to ${url}...`);

let ws: WebSocket;
let currentSessionId: string | null = null;

function connect() {
  ws = new WebSocket(url);

  ws.onopen = () => {
    console.log("Connected!\n");
    console.log("Type 'help' for available commands.\n");
    startRepl();
  };

  ws.onmessage = (event) => {
    const msg = JSON.parse(event.data as string) as ServerMessage;
    handleMessage(msg);
  };

  ws.onerror = (error) => {
    console.error("WebSocket error:", error);
  };

  ws.onclose = () => {
    console.log("\nDisconnected from server");
    process.exit(1);
  };
}

function send(msg: ClientMessage) {
  ws.send(JSON.stringify(msg));
}

function handleMessage(msg: ServerMessage) {
  switch (msg.type) {
    case "session.created":
      console.log(`\n[SESSION CREATED] id=${msg.session.id} status=${msg.session.status}`);
      currentSessionId = msg.session.id;
      break;

    case "session.updated":
      console.log(`\n[SESSION UPDATED] id=${msg.session.id} status=${msg.session.status}`);
      break;

    case "session.deleted":
      console.log(`\n[SESSION DELETED] id=${msg.id}`);
      if (currentSessionId === msg.id) currentSessionId = null;
      break;

    case "session.list":
      console.log("\n[SESSIONS]");
      if (msg.sessions.length === 0) {
        console.log("  (no sessions)");
      } else {
        for (const s of msg.sessions) {
          console.log(`  ${s.id} - ${s.name || "(unnamed)"} - ${s.status}`);
        }
      }
      break;

    case "session.error":
      console.log(`\n[SESSION ERROR] ${msg.id ? `id=${msg.id} ` : ""}${msg.error}`);
      break;

    case "agent.event":
      const evt = msg.event;
      if (evt.type === "tool_call") {
        console.log(`\n[TOOL] ${evt.tool}: ${JSON.stringify(evt.input || {}).slice(0, 100)}...`);
      } else if (evt.type === "tool_result") {
        console.log(`[TOOL RESULT] ${(evt.content || "").slice(0, 100)}...`);
      } else {
        console.log(`\n[EVENT] ${JSON.stringify(evt).slice(0, 150)}...`);
      }
      break;

    case "agent.message":
      console.log(`\n[AGENT] ${msg.content}`);
      break;

    case "agent.done":
      console.log(`\n[AGENT DONE] Session ${msg.sessionId}`);
      break;

    case "agent.error":
      console.log(`\n[AGENT ERROR] ${msg.error}`);
      break;

    case "error":
      console.log(`\n[ERROR] ${msg.message}`);
      break;

    default:
      console.log(`\n[UNKNOWN] ${JSON.stringify(msg)}`);
  }
}

function startRepl() {
  const rl = readline.createInterface({
    input: process.stdin,
    output: process.stdout,
    prompt: "netclode> ",
  });

  rl.prompt();

  rl.on("line", (line) => {
    const parts = line.trim().split(/\s+/);
    const cmd = parts[0]?.toLowerCase();

    switch (cmd) {
      case "":
        break;

      case "help":
        console.log(`
Commands:
  create [name]       Create a new session
  list                List all sessions
  resume <id>         Resume a session
  pause <id>          Pause a session
  delete <id>         Delete a session
  use <id>            Set current session
  prompt <text>       Send prompt to current session (or: p <text>)
  prompt <id> <text>  Send prompt to specific session
  interrupt [id]      Interrupt current prompt
  quit                Exit
`);
        break;

      case "create":
        const name = parts.slice(1).join(" ") || undefined;
        send({ type: "session.create", name });
        console.log("Creating session...");
        break;

      case "list":
      case "ls":
        send({ type: "session.list" });
        break;

      case "resume":
        if (!parts[1]) {
          console.log("Usage: resume <session-id>");
        } else {
          send({ type: "session.resume", id: parts[1] });
          console.log(`Resuming session ${parts[1]}...`);
        }
        break;

      case "pause":
        if (!parts[1]) {
          console.log("Usage: pause <session-id>");
        } else {
          send({ type: "session.pause", id: parts[1] });
        }
        break;

      case "delete":
      case "rm":
        if (!parts[1]) {
          console.log("Usage: delete <session-id>");
        } else {
          send({ type: "session.delete", id: parts[1] });
        }
        break;

      case "use":
        if (!parts[1]) {
          console.log(`Current session: ${currentSessionId || "(none)"}`);
        } else {
          currentSessionId = parts[1];
          console.log(`Switched to session ${currentSessionId}`);
        }
        break;

      case "p":
      case "prompt":
        // Check if first arg looks like a session ID (contains hyphen)
        if (parts[1]?.includes("-") && parts.length > 2) {
          // prompt <id> <text>
          const sessionId = parts[1];
          const text = parts.slice(2).join(" ");
          send({ type: "prompt", sessionId, text });
          console.log(`Sending prompt to ${sessionId}...`);
        } else if (currentSessionId) {
          // prompt <text> using current session
          const text = parts.slice(1).join(" ");
          if (!text) {
            console.log("Usage: prompt <text>");
          } else {
            send({ type: "prompt", sessionId: currentSessionId, text });
            console.log(`Sending prompt...`);
          }
        } else {
          console.log("No current session. Use 'use <id>' or 'prompt <id> <text>'");
        }
        break;

      case "interrupt":
      case "stop":
        const intId = parts[1] || currentSessionId;
        if (!intId) {
          console.log("Usage: interrupt <session-id>");
        } else {
          send({ type: "prompt.interrupt", sessionId: intId });
          console.log(`Interrupting ${intId}...`);
        }
        break;

      case "quit":
      case "exit":
      case "q":
        console.log("Bye!");
        process.exit(0);
        break;

      default:
        console.log(`Unknown command: ${cmd}. Type 'help' for commands.`);
    }

    rl.prompt();
  });

  rl.on("close", () => {
    console.log("\nBye!");
    process.exit(0);
  });
}

connect();
