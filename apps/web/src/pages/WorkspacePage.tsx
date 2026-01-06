import { useParams, useNavigate } from "react-router-dom";
import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import {
  useWebSocket,
  useWebSocketMessages,
} from "../contexts/WebSocketContext";
import { useSessionStore } from "../stores/sessionStore";
import { ChatPanel } from "../components/ChatPanel";
import { Terminal } from "../components/Terminal";
import type { ServerMessage } from "@netclode/protocol";
import styles from "./WorkspacePage.module.css";

export function WorkspacePage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [isProcessing, setIsProcessing] = useState(false);
  const [_historyLoaded, setHistoryLoaded] = useState(false);
  const lastOpenedIdRef = useRef<string | null>(null);
  const {
    sessions,
    updateSession,
    messagesBySession,
    eventsBySession,
    appendMessage,
    appendAssistantPartial,
    appendEvent,
    clearEvents,
    setMessages,
    setEvents,
  } = useSessionStore();
  const { send, connected } = useWebSocket();

  const session = useMemo(
    () => sessions.find((item) => item.id === id) ?? null,
    [sessions, id]
  );

  const messages = id ? messagesBySession[id] ?? [] : [];
  const events = id ? eventsBySession[id] ?? [] : [];

  const handleMessage = useCallback(
    (msg: ServerMessage) => {
      switch (msg.type) {
        case "session.updated":
          updateSession(msg.session);
          break;
        case "session.state":
          // Server sync response - load history from server
          if (msg.session.id === id) {
            updateSession(msg.session);
            setMessages(msg.session.id, msg.messages);
            setEvents(msg.session.id, msg.events);
            setHistoryLoaded(true);
          }
          break;
        case "agent.message":
          if (msg.sessionId === id) {
            if (msg.partial) {
              appendAssistantPartial(msg.sessionId, msg.content);
            } else {
              appendMessage(msg.sessionId, {
                role: "assistant",
                content: msg.content,
              });
            }
          }
          break;
        case "agent.event":
          if (msg.sessionId === id) {
            appendEvent(msg.sessionId, msg.event);
          }
          break;
        case "agent.done":
          if (msg.sessionId === id) {
            setIsProcessing(false);
          }
          break;
        case "agent.error":
          if (msg.sessionId === id) {
            appendMessage(msg.sessionId, {
              role: "assistant",
              content: `Error: ${msg.error}`,
            });
            setIsProcessing(false);
          }
          break;
      }
    },
    [
      id,
      updateSession,
      appendMessage,
      appendAssistantPartial,
      appendEvent,
      setMessages,
      setEvents,
    ]
  );

  useWebSocketMessages(handleMessage);

  // Open session and load history when entering workspace
  useEffect(() => {
    if (connected && id && lastOpenedIdRef.current !== id) {
      lastOpenedIdRef.current = id;
      setHistoryLoaded(false);
      // Send session.open to get history and subscribe
      send({ type: "session.open", id });
      // Also resume the session to ensure sandbox is running
      send({ type: "session.resume", id });
    }
  }, [connected, id, send]);

  const handleSendPrompt = (text: string) => {
    if (!id) return;
    appendMessage(id, { role: "user", content: text });
    setIsProcessing(true);
    clearEvents(id);
    send({ type: "prompt", sessionId: id, text });
  };

  const handleInterrupt = () => {
    if (!id) return;
    send({ type: "prompt.interrupt", sessionId: id });
  };

  const handleTerminalInput = (data: string) => {
    if (!id) return;
    send({ type: "terminal.input", sessionId: id, data });
  };

  return (
    <div className={styles.container}>
      <header className={styles.header}>
        <button className={styles.backButton} onClick={() => navigate("/")}>
          ← Back
        </button>
        <span className={styles.sessionId}>
          {session?.name || `Session ${id?.slice(0, 8)}`}
        </span>
        <span className={styles.status} data-status={session?.status}>
          {connected
            ? isProcessing
              ? "Processing..."
              : session?.status || "Connecting..."
            : "Disconnected"}
        </span>
      </header>
      <main className={styles.main}>
        <div className={styles.chatSection}>
          <ChatPanel
            messages={messages}
            events={events}
            onSend={handleSendPrompt}
            onInterrupt={handleInterrupt}
            disabled={!connected || session?.status !== "running"}
            isProcessing={isProcessing}
          />
        </div>
        <div className={styles.terminalSection}>
          <Terminal onInput={handleTerminalInput} />
        </div>
      </main>
    </div>
  );
}
