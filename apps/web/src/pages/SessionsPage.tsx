import { useEffect, useCallback, useState, useMemo } from "react";
import { useLocation } from "wouter";
import {
  Container,
  Group,
  Title,
  Button,
  Badge,
  Stack,
  AppShell,
} from "@mantine/core";
import { SessionList } from "../components/SessionList";
import { ThemeToggle } from "../components/ThemeToggle";
import {
  useWebSocket,
  useWebSocketMessages,
} from "../contexts/WebSocketContext";
import type { ServerMessage, Session } from "@netclode/protocol";

export function SessionsPage() {
  const [, navigate] = useLocation();
  const [sessions, setSessions] = useState<Session[]>([]);
  const { send, connected } = useWebSocket();
  const [creating, setCreating] = useState(false);

  // Sort sessions by lastActiveAt descending (most recent first)
  const sortedSessions = useMemo(
    () =>
      [...sessions].sort(
        (a, b) =>
          new Date(b.lastActiveAt).getTime() -
          new Date(a.lastActiveAt).getTime()
      ),
    [sessions]
  );

  const handleMessage = useCallback(
    (msg: ServerMessage) => {
      if (msg.type === "session.list") {
        setSessions(msg.sessions);
      } else if (msg.type === "session.created") {
        setCreating(false);
        setSessions((prev) => [...prev, msg.session]);
        navigate(`/session/${msg.session.id}`);
      } else if (msg.type === "session.updated") {
        setSessions((prev) =>
          prev.some((s) => s.id === msg.session.id)
            ? prev.map((s) => (s.id === msg.session.id ? msg.session : s))
            : [...prev, msg.session]
        );
      } else if (msg.type === "session.deleted") {
        setSessions((prev) => prev.filter((s) => s.id !== msg.id));
      } else if (msg.type === "session.error") {
        setCreating(false);
      }
    },
    [navigate]
  );

  useWebSocketMessages(handleMessage);

  useEffect(() => {
    if (connected) {
      send({ type: "session.list" });
    }
  }, [connected, send]);

  useEffect(() => {
    if (!connected) {
      setCreating(false);
    }
  }, [connected]);

  const handleCreateSession = () => {
    if (!connected || creating) return;
    setCreating(true);
    send({ type: "session.create" });
  };

  const handleDeleteSession = (id: string) => {
    if (!connected) return;
    send({ type: "session.delete", id });
  };

  return (
    <AppShell header={{ height: 60 }} padding="md">
      <AppShell.Header>
        <Group h="100%" px="md" justify="space-between">
          <Group>
            <Title order={3}>Netclode</Title>
            <Badge
              color={connected ? "green" : "gray"}
              variant="dot"
              size="lg"
            >
              {connected ? "Connected" : "Disconnected"}
            </Badge>
          </Group>
          <ThemeToggle />
        </Group>
      </AppShell.Header>

      <AppShell.Main>
        <Container size="sm">
          <Stack gap="md">
            <SessionList
              sessions={sortedSessions}
              onSelect={(id) => navigate(`/session/${id}`)}
              onDelete={handleDeleteSession}
            />
            <Button
              onClick={handleCreateSession}
              disabled={!connected || creating}
              loading={creating}
              size="lg"
              fullWidth
            >
              {creating ? "Creating..." : "New Session"}
            </Button>
          </Stack>
        </Container>
      </AppShell.Main>
    </AppShell>
  );
}
