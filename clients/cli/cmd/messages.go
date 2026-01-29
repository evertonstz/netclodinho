package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/angristan/netclode/clients/cli/internal/client"
	"github.com/angristan/netclode/clients/cli/internal/output"
	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/spf13/cobra"
)

var (
	messagesLimit int
	messagesRole  string
)

var messagesCmd = &cobra.Command{
	Use:   "messages <session-id>",
	Short: "Show chat messages for a session",
	Args:  cobra.ExactArgs(1),
	RunE:  runMessages,
}

func init() {
	rootCmd.AddCommand(messagesCmd)
	messagesCmd.Flags().IntVarP(&messagesLimit, "limit", "n", 0, "Limit number of messages (0 = all)")
	messagesCmd.Flags().StringVar(&messagesRole, "role", "", "Filter by role (user, assistant)")
}

// MessageInfo contains extracted message data from AgentEvent.
type MessageInfo struct {
	Role      pb.MessageRole
	Content   string
	Timestamp string
}

func runMessages(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c := client.New(getServerURL())
	sessionID := args[0]

	state, err := c.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}

	// Extract messages from entries (AgentEvents with MESSAGE kind)
	var messages []MessageInfo
	for _, e := range state.Entries {
		if e.Partial {
			continue // Skip streaming deltas
		}
		if event := e.GetEvent(); event != nil && event.Kind == pb.AgentEventKind_AGENT_EVENT_KIND_MESSAGE {
			if msg := event.GetMessage(); msg != nil {
				messages = append(messages, MessageInfo{
					Role:      msg.Role,
					Content:   msg.Content,
					Timestamp: e.Timestamp.AsTime().Format("15:04:05"),
				})
			}
		}
	}

	// Filter by role if specified
	if messagesRole != "" {
		filtered := make([]MessageInfo, 0)
		targetRole := strings.ToUpper(messagesRole)
		if !strings.HasPrefix(targetRole, "MESSAGE_ROLE_") {
			targetRole = "MESSAGE_ROLE_" + targetRole
		}
		for _, m := range messages {
			if m.Role.String() == targetRole {
				filtered = append(filtered, m)
			}
		}
		messages = filtered
	}

	// Apply limit (from the end)
	if messagesLimit > 0 && len(messages) > messagesLimit {
		messages = messages[len(messages)-messagesLimit:]
	}

	if isJSONOutput() {
		return output.JSON(messages)
	}

	if len(messages) == 0 {
		fmt.Println("No messages found.")
		return nil
	}

	printMessages(messages)
	return nil
}

func printMessages(messages []MessageInfo) {
	for i, msg := range messages {
		if i > 0 {
			fmt.Println()
		}

		role := formatRole(msg.Role.String())
		timestamp := msg.Timestamp

		// Role header with color
		var roleColor = output.MutedColor
		if strings.Contains(strings.ToLower(role), "user") {
			roleColor = output.SuccessColor
		} else if strings.Contains(strings.ToLower(role), "assistant") {
			roleColor = output.IDColor
		}

		_, _ = roleColor.Printf("[%s] ", role)
		_, _ = output.TimeColor.Printf("%s\n", timestamp)

		// Message content (indented)
		lines := strings.Split(msg.Content, "\n")
		for _, line := range lines {
			fmt.Printf("  %s\n", line)
		}
	}
}

func formatRole(role string) string {
	// Convert "MESSAGE_ROLE_USER" -> "user"
	s := strings.ToLower(role)
	s = strings.TrimPrefix(s, "message_role_")
	return s
}
