package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/angristan/netclode/clients/cli/internal/client"
	"github.com/angristan/netclode/clients/cli/internal/output"
	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/spf13/cobra"
)

var (
	eventsLimit int
	eventsKind  string
)

var eventsCmd = &cobra.Command{
	Use:   "events <session-id>",
	Short: "Show events for a session",
	Args:  cobra.ExactArgs(1),
	RunE:  runEvents,
}

var eventsTailCmd = &cobra.Command{
	Use:   "tail <session-id>",
	Short: "Stream events in real-time",
	Args:  cobra.ExactArgs(1),
	RunE:  runEventsTail,
}

func init() {
	rootCmd.AddCommand(eventsCmd)
	eventsCmd.AddCommand(eventsTailCmd)
	eventsCmd.Flags().IntVarP(&eventsLimit, "limit", "n", 50, "Limit number of events (0 = all)")
	eventsCmd.Flags().StringVar(&eventsKind, "kind", "", "Filter by event kind (TOOL_START, FILE_CHANGE, etc.)")
}

func runEvents(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c := client.New(getServerURL())
	sessionID := args[0]

	state, err := c.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}

	events := state.Events

	// Filter by kind if specified
	if eventsKind != "" {
		filtered := make([]*pb.PersistedEvent, 0)
		targetKind := normalizeEventKind(eventsKind)
		for _, e := range events {
			if e.Event != nil && formatEventKind(e.Event.Kind) == targetKind {
				filtered = append(filtered, e)
			}
		}
		events = filtered
	}

	// Apply limit (from the end)
	if eventsLimit > 0 && len(events) > eventsLimit {
		events = events[len(events)-eventsLimit:]
	}

	if isJSONOutput() {
		return output.JSON(events)
	}

	if len(events) == 0 {
		fmt.Println("No events found.")
		return nil
	}

	printEventsTable(events)
	return nil
}

func normalizeEventKind(kind string) string {
	// Normalize user input to match our formatted output
	k := strings.ToLower(kind)
	k = strings.TrimPrefix(k, "agent_event_kind_")
	return k
}

func formatEventKind(kind pb.AgentEventKind) string {
	// Convert "AGENT_EVENT_KIND_TOOL_START" -> "tool_start"
	s := kind.String()
	s = strings.ToLower(s)
	s = strings.TrimPrefix(s, "agent_event_kind_")
	return s
}

func printEventsTable(events []*pb.PersistedEvent) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	_, _ = output.HeaderColor.Fprintf(w, "TIME\tKIND\tTOOL/PATH\tDETAILS\n")

	for _, pe := range events {
		e := pe.Event
		if e == nil {
			continue
		}

		timestamp := output.FormatTimestamp(pe.Timestamp)
		kind := formatEventKind(e.Kind)
		toolOrPath := getToolOrPath(e)
		details := getEventDetails(e)

		kindColor := output.EventKindColor(kind)

		_, _ = output.TimeColor.Fprintf(w, "%s\t", timestamp)
		_, _ = kindColor.Fprintf(w, "%s\t", kind)
		_, _ = fmt.Fprintf(w, "%s\t", output.Truncate(toolOrPath, 30))
		_, _ = fmt.Fprintf(w, "%s\n", output.Truncate(details, 50))
	}

	_ = w.Flush()
}

func getToolOrPath(e *pb.AgentEvent) string {
	if tool := e.GetTool(); tool != nil {
		return tool.Tool
	}
	if fc := e.GetFileChange(); fc != nil {
		return fc.Path
	}
	if cmd := e.GetCommand(); cmd != nil {
		return output.Truncate(cmd.Command, 30)
	}
	return "-"
}

func getEventDetails(e *pb.AgentEvent) string {
	switch e.Kind {
	case pb.AgentEventKind_AGENT_EVENT_KIND_TOOL_START,
		pb.AgentEventKind_AGENT_EVENT_KIND_TOOL_END,
		pb.AgentEventKind_AGENT_EVENT_KIND_TOOL_INPUT,
		pb.AgentEventKind_AGENT_EVENT_KIND_TOOL_INPUT_COMPLETE:
		if tool := e.GetTool(); tool != nil {
			if tool.Result != nil {
				return *tool.Result
			}
			if tool.Error != nil {
				return "error: " + *tool.Error
			}
			// Show input fields
			if tool.Input != nil {
				var parts []string
				for k, v := range tool.Input.AsMap() {
					parts = append(parts, fmt.Sprintf("%s=%v", k, v))
				}
				return strings.Join(parts, " ")
			}
		}

	case pb.AgentEventKind_AGENT_EVENT_KIND_FILE_CHANGE:
		if fc := e.GetFileChange(); fc != nil {
			action := fc.Action.String()
			action = strings.ToLower(strings.TrimPrefix(action, "FILE_ACTION_"))
			lines := ""
			if fc.LinesAdded != nil || fc.LinesRemoved != nil {
				added := int32(0)
				removed := int32(0)
				if fc.LinesAdded != nil {
					added = *fc.LinesAdded
				}
				if fc.LinesRemoved != nil {
					removed = *fc.LinesRemoved
				}
				lines = fmt.Sprintf(" +%d/-%d", added, removed)
			}
			return action + lines
		}

	case pb.AgentEventKind_AGENT_EVENT_KIND_COMMAND_START,
		pb.AgentEventKind_AGENT_EVENT_KIND_COMMAND_END:
		if cmd := e.GetCommand(); cmd != nil {
			if cmd.ExitCode != nil {
				return fmt.Sprintf("exit=%d", *cmd.ExitCode)
			}
		}

	case pb.AgentEventKind_AGENT_EVENT_KIND_THINKING:
		if thinking := e.GetThinking(); thinking != nil {
			return output.Truncate(thinking.Content, 50)
		}

	case pb.AgentEventKind_AGENT_EVENT_KIND_PORT_EXPOSED:
		if port := e.GetPortExposed(); port != nil {
			url := ""
			if port.PreviewUrl != nil {
				url = " " + *port.PreviewUrl
			}
			return fmt.Sprintf("port=%d%s", port.Port, url)
		}

	case pb.AgentEventKind_AGENT_EVENT_KIND_REPO_CLONE:
		if repo := e.GetRepoClone(); repo != nil {
			stage := repo.Stage.String()
			stage = strings.ToLower(strings.TrimPrefix(stage, "REPO_CLONE_STAGE_"))
			return stage + " " + repo.Message
		}
	}

	return ""
}

func runEventsTail(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	c := client.New(getServerURL())
	sessionID := args[0]

	fmt.Fprintf(os.Stderr, "Tailing events for session %s (Ctrl+C to stop)...\n\n", sessionID)

	err := c.TailEvents(ctx, sessionID,
		func(event *pb.AgentEventResponse) error {
			if isJSONOutput() {
				return output.JSONLine(event)
			}
			printStreamEvent(event)
			return nil
		},
		func(msg *pb.AgentMessageResponse) error {
			if isJSONOutput() {
				return output.JSONLine(msg)
			}
			// Only print final messages, not partials
			if !msg.Partial {
				printStreamMessage(msg)
			}
			return nil
		},
	)

	if err != nil && ctx.Err() == nil {
		return err
	}

	return nil
}

func printStreamEvent(resp *pb.AgentEventResponse) {
	e := resp.Event
	if e == nil {
		return
	}

	timestamp := output.FormatTimestamp(e.Timestamp)
	kind := formatEventKind(e.Kind)
	toolOrPath := getToolOrPath(e)
	details := getEventDetails(e)

	kindColor := output.EventKindColor(kind)

	_, _ = output.TimeColor.Printf("[%s] ", timestamp)
	_, _ = kindColor.Printf("%-15s ", kind)
	fmt.Printf("%-20s ", output.Truncate(toolOrPath, 20))
	_, _ = output.MutedColor.Printf("%s\n", details)
}

func printStreamMessage(msg *pb.AgentMessageResponse) {
	_, _ = output.TimeColor.Print("[message] ")
	_, _ = output.IDColor.Printf("assistant: ")
	// Show first line only for streaming view
	content := msg.Content
	if idx := strings.Index(content, "\n"); idx > 0 {
		content = content[:idx] + "..."
	}
	fmt.Println(output.Truncate(content, 60))
}
