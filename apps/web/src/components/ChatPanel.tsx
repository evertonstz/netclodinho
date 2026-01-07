import { useState, useRef, useEffect } from "react";
import {
  Box,
  Paper,
  Text,
  Textarea,
  ActionIcon,
  Group,
  Stack,
  ScrollArea,
  Collapse,
  Badge,
  Code,
  Loader,
  Anchor,
  useMantineColorScheme,
} from "@mantine/core";
import type { AgentEvent } from "@netclode/protocol";

export interface ChatMessage {
  role: "user" | "assistant";
  content: string;
}

function EventDetails({ event }: { event: AgentEvent }) {
  switch (event.kind) {
    case "tool_start":
      return (
        <Stack gap="xs" p="xs">
          <Group gap="xs">
            <Text size="xs" c="dimmed">Input:</Text>
          </Group>
          <Code block style={{ fontSize: 11, maxHeight: 200, overflow: "auto" }}>
            {JSON.stringify(event.input, null, 2)}
          </Code>
        </Stack>
      );
    case "tool_end":
      return (
        <Stack gap="xs" p="xs">
          {event.result && (
            <>
              <Text size="xs" c="dimmed">Result:</Text>
              <Code block style={{ fontSize: 11, maxHeight: 200, overflow: "auto" }}>
                {event.result}
              </Code>
            </>
          )}
          {event.error && (
            <>
              <Text size="xs" c="red">Error:</Text>
              <Code block color="red" style={{ fontSize: 11 }}>
                {event.error}
              </Code>
            </>
          )}
        </Stack>
      );
    case "file_change":
      return (
        <Stack gap="xs" p="xs">
          <Group gap="xs">
            <Text size="xs" c="dimmed">Path:</Text>
            <Code style={{ fontSize: 11 }}>{event.path}</Code>
          </Group>
          <Group gap="xs">
            <Text size="xs" c="dimmed">Action:</Text>
            <Text size="xs">{event.action}</Text>
          </Group>
          {(event.linesAdded !== undefined || event.linesRemoved !== undefined) && (
            <Group gap="xs">
              <Text size="xs" c="dimmed">Changes:</Text>
              {event.linesAdded !== undefined && (
                <Text size="xs" c="green">+{event.linesAdded}</Text>
              )}
              {event.linesRemoved !== undefined && (
                <Text size="xs" c="red">-{event.linesRemoved}</Text>
              )}
            </Group>
          )}
        </Stack>
      );
    case "command_start":
      return (
        <Stack gap="xs" p="xs">
          <Text size="xs" c="dimmed">Command:</Text>
          <Code block style={{ fontSize: 11 }}>{event.command}</Code>
          {event.cwd && (
            <Group gap="xs">
              <Text size="xs" c="dimmed">CWD:</Text>
              <Code style={{ fontSize: 11 }}>{event.cwd}</Code>
            </Group>
          )}
        </Stack>
      );
    case "command_end":
      return (
        <Stack gap="xs" p="xs">
          <Group gap="xs">
            <Text size="xs" c="dimmed">Exit code:</Text>
            <Text size="xs" c={event.exitCode === 0 ? "green" : "red"}>
              {event.exitCode}
            </Text>
          </Group>
          {event.output && (
            <>
              <Text size="xs" c="dimmed">Output:</Text>
              <Code block style={{ fontSize: 11, maxHeight: 200, overflow: "auto" }}>
                {event.output}
              </Code>
            </>
          )}
        </Stack>
      );
    case "thinking":
      return (
        <Box p="xs">
          <Text size="xs" c="dimmed" style={{ whiteSpace: "pre-wrap" }}>
            {event.content}
          </Text>
        </Box>
      );
    case "port_detected":
      return (
        <Stack gap="xs" p="xs">
          <Group gap="xs">
            <Text size="xs" c="dimmed">Port:</Text>
            <Text size="xs">{event.port}</Text>
          </Group>
          {event.process && (
            <Group gap="xs">
              <Text size="xs" c="dimmed">Process:</Text>
              <Text size="xs">{event.process}</Text>
            </Group>
          )}
          {event.previewUrl && (
            <Group gap="xs">
              <Text size="xs" c="dimmed">Preview:</Text>
              <Anchor href={event.previewUrl} target="_blank" size="xs">
                {event.previewUrl}
              </Anchor>
            </Group>
          )}
        </Stack>
      );
    default:
      return null;
  }
}

function getEventIcon(kind: AgentEvent["kind"]): string {
  switch (kind) {
    case "tool_start": return "🔧";
    case "tool_end": return "✓";
    case "file_change": return "📄";
    case "command_start": return "▶";
    case "command_end": return "■";
    case "thinking": return "💭";
    case "port_detected": return "🌐";
    default: return "•";
  }
}

function getEventSummary(event: AgentEvent): string {
  switch (event.kind) {
    case "tool_start":
      return event.tool;
    case "tool_end":
      return `${event.tool}${event.error ? " (error)" : ""}`;
    case "file_change":
      return `${event.action} ${event.path.split("/").pop()}`;
    case "command_start":
      return (event.command ?? "").slice(0, 40) + ((event.command?.length ?? 0) > 40 ? "..." : "");
    case "command_end":
      return `exit ${event.exitCode}`;
    case "thinking":
      return (event.content ?? "").slice(0, 40) + ((event.content?.length ?? 0) > 40 ? "..." : "");
    case "port_detected":
      return `Port ${event.port}`;
    default:
      return "";
  }
}

interface ChatPanelProps {
  messages: ChatMessage[];
  events: AgentEvent[];
  onSend: (text: string) => void;
  onInterrupt?: () => void;
  disabled?: boolean;
  isProcessing?: boolean;
}

export function ChatPanel({
  messages,
  events,
  onSend,
  onInterrupt,
  disabled,
  isProcessing,
}: ChatPanelProps) {
  const [input, setInput] = useState("");
  const [expandedEvents, setExpandedEvents] = useState<Set<number>>(new Set());
  const viewport = useRef<HTMLDivElement>(null);
  const { colorScheme } = useMantineColorScheme();
  const isDark = colorScheme === "dark";

  const userMsgBg = isDark ? "blue.9" : "blue.0";
  const assistantMsgBg = isDark ? "orange.9" : "orange.0";

  const toggleEvent = (index: number) => {
    setExpandedEvents((prev) => {
      const next = new Set(prev);
      if (next.has(index)) {
        next.delete(index);
      } else {
        next.add(index);
      }
      return next;
    });
  };

  useEffect(() => {
    viewport.current?.scrollTo({ top: viewport.current.scrollHeight, behavior: "smooth" });
  }, [messages, events]);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (input.trim() && !disabled && !isProcessing) {
      onSend(input.trim());
      setInput("");
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSubmit(e);
    }
  };

  return (
    <Box h="100%" style={{ display: "flex", flexDirection: "column" }}>
      <ScrollArea flex={1} viewportRef={viewport} p="md">
        {messages.length === 0 && (
          <Paper p="xl" ta="center" withBorder>
            <Text size="xl" mb="xs">💬</Text>
            <Text c="dimmed">Ask Claude anything...</Text>
          </Paper>
        )}
        <Stack gap="md">
          {messages.map((msg, i) => (
            <Group key={i} align="flex-start" gap="sm" wrap="nowrap">
              <Text size="lg">{msg.role === "user" ? "👤" : "🧠"}</Text>
              <Box style={{ flex: 1, minWidth: 0 }}>
                <Text size="xs" fw={500} mb={4}>
                  {msg.role === "user" ? "You" : "Claude"}
                </Text>
                <Paper
                  p="sm"
                  bg={msg.role === "user" ? userMsgBg : assistantMsgBg}
                  radius="md"
                >
                  <Text size="sm" style={{ whiteSpace: "pre-wrap" }}>
                    {msg.content}
                  </Text>
                </Paper>
              </Box>
            </Group>
          ))}
          {events.length > 0 && (
            <Paper withBorder p="sm">
              <Group gap="xs" mb="sm">
                <Text size="sm">⚡</Text>
                <Text size="sm" fw={500}>Activity ({events.length})</Text>
              </Group>
              <Stack gap="xs">
                {events.map((event, i) => {
                  const isExpanded = expandedEvents.has(i);
                  return (
                    <Paper
                      key={i}
                      withBorder
                      p="xs"
                      style={{ cursor: "pointer" }}
                      onClick={() => toggleEvent(i)}
                    >
                      <Group gap="xs" wrap="nowrap">
                        <Text size="sm">{getEventIcon(event.kind)}</Text>
                        <Badge size="xs" variant="light">{event.kind}</Badge>
                        <Text size="xs" c="dimmed" truncate style={{ flex: 1 }}>
                          {getEventSummary(event)}
                        </Text>
                        <Text size="xs" c="dimmed">{isExpanded ? "▼" : "▶"}</Text>
                      </Group>
                      <Collapse in={isExpanded}>
                        <EventDetails event={event} />
                      </Collapse>
                    </Paper>
                  );
                })}
              </Stack>
            </Paper>
          )}
          {isProcessing && (
            <Group align="flex-start" gap="sm">
              <Text size="lg">🧠</Text>
              <Loader size="sm" type="dots" />
            </Group>
          )}
        </Stack>
      </ScrollArea>
      <Box p="md" style={{ borderTop: "1px solid var(--mantine-color-default-border)" }}>
        <form onSubmit={handleSubmit}>
          <Group gap="sm" align="flex-end">
            <Textarea
              flex={1}
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder={disabled ? "Session not ready..." : "Ask Claude..."}
              disabled={disabled}
              minRows={1}
              maxRows={4}
              autosize
            />
            {isProcessing ? (
              <ActionIcon
                size="lg"
                variant="filled"
                color="red"
                onClick={onInterrupt}
                title="Stop"
              >
                ■
              </ActionIcon>
            ) : (
              <ActionIcon
                size="lg"
                variant="filled"
                type="submit"
                disabled={disabled || !input.trim()}
                title="Send"
              >
                ↑
              </ActionIcon>
            )}
          </Group>
        </form>
      </Box>
    </Box>
  );
}
