import { create } from "zustand";
import { persist } from "zustand/middleware";
import type { AgentEvent, Session } from "@netclode/protocol";

export interface ChatMessage {
  role: "user" | "assistant";
  content: string;
}

interface SessionStore {
  sessions: Session[];
  currentSessionId: string | null;
  messagesBySession: Record<string, ChatMessage[]>;
  eventsBySession: Record<string, AgentEvent[]>;
  setSessions: (sessions: Session[]) => void;
  addSession: (session: Session) => void;
  updateSession: (session: Session) => void;
  removeSession: (id: string) => void;
  setCurrentSession: (id: string | null) => void;
  appendMessage: (sessionId: string, message: ChatMessage) => void;
  appendAssistantPartial: (sessionId: string, delta: string) => void;
  appendEvent: (sessionId: string, event: AgentEvent) => void;
  clearEvents: (sessionId: string) => void;
}

export const useSessionStore = create<SessionStore>()(
  persist(
    (set) => ({
      sessions: [],
      currentSessionId: null,
      messagesBySession: {},
      eventsBySession: {},

      setSessions: (sessions) => set({ sessions }),

      addSession: (session) =>
        set((state) => ({
          sessions: [...state.sessions, session],
        })),

      updateSession: (session) =>
        set((state) => {
          const exists = state.sessions.some((s) => s.id === session.id);
          return {
            sessions: exists
              ? state.sessions.map((s) =>
                  s.id === session.id ? session : s
                )
              : [...state.sessions, session],
          };
        }),

      removeSession: (id) =>
        set((state) => ({
          sessions: state.sessions.filter((s) => s.id !== id),
          messagesBySession: Object.fromEntries(
            Object.entries(state.messagesBySession).filter(
              ([key]) => key !== id
            )
          ),
          eventsBySession: Object.fromEntries(
            Object.entries(state.eventsBySession).filter(
              ([key]) => key !== id
            )
          ),
        })),

      setCurrentSession: (id) => set({ currentSessionId: id }),

      appendMessage: (sessionId, message) =>
        set((state) => {
          const prev = state.messagesBySession[sessionId] ?? [];
          return {
            messagesBySession: {
              ...state.messagesBySession,
              [sessionId]: [...prev, message],
            },
          };
        }),

      appendAssistantPartial: (sessionId, delta) =>
        set((state) => {
          const prev = state.messagesBySession[sessionId] ?? [];
          const last = prev[prev.length - 1];
          const next =
            last?.role === "assistant"
              ? [
                  ...prev.slice(0, -1),
                  { ...last, content: last.content + delta },
                ]
              : [...prev, { role: "assistant", content: delta }];
          return {
            messagesBySession: {
              ...state.messagesBySession,
              [sessionId]: next,
            },
          };
        }),

      appendEvent: (sessionId, event) =>
        set((state) => {
          const prev = state.eventsBySession[sessionId] ?? [];
          return {
            eventsBySession: {
              ...state.eventsBySession,
              [sessionId]: [...prev, event],
            },
          };
        }),

      clearEvents: (sessionId) =>
        set((state) => ({
          eventsBySession: {
            ...state.eventsBySession,
            [sessionId]: [],
          },
        })),
    }),
    {
      name: "netclode-session-store",
      partialize: (state) => ({
        messagesBySession: state.messagesBySession,
      }),
    }
  )
);
