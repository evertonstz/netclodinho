package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/angristan/netclode/clients/cli/internal/client"
	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/spf13/cobra"
)

var promptCmd = &cobra.Command{
	Use:   "prompt <session-id> <text>",
	Short: "Send a prompt to a session (for testing)",
	Args:  cobra.ExactArgs(2),
	RunE:  runPrompt,
}

func init() {
	rootCmd.AddCommand(promptCmd)
}

func runPrompt(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	c := client.New(getServerURL())
	sessionID := args[0]
	text := args[1]

	stream := c.Stream(ctx)
	defer func() { _ = stream.CloseRequest() }()

	// Open session first
	if err := stream.Send(&pb.ClientMessage{
		Message: &pb.ClientMessage_OpenSession{
			OpenSession: &pb.OpenSessionRequest{
				SessionId: sessionID,
			},
		},
	}); err != nil {
		return fmt.Errorf("open session: %w", err)
	}

	// Wait for session state
	msg, err := stream.Receive()
	if err != nil {
		return fmt.Errorf("receive session state: %w", err)
	}
	if errResp := msg.GetError(); errResp != nil {
		return fmt.Errorf("%s: %s", errResp.Error.Code, errResp.Error.Message)
	}
	if msg.GetSessionState() == nil {
		return fmt.Errorf("expected session state, got %T", msg.GetMessage())
	}

	fmt.Printf("Session opened, sending prompt: %q\n", text)

	// Send prompt
	if err := stream.Send(&pb.ClientMessage{
		Message: &pb.ClientMessage_SendPrompt{
			SendPrompt: &pb.SendPromptRequest{
				SessionId: sessionID,
				Text:      text,
			},
		},
	}); err != nil {
		return fmt.Errorf("send prompt: %w", err)
	}

	fmt.Println("Waiting for response...")

	// Collect response
	var response strings.Builder
	for {
		msg, err := stream.Receive()
		if err != nil {
			return fmt.Errorf("receive: %w", err)
		}

		if agentMsg := msg.GetAgentMessage(); agentMsg != nil {
			response.WriteString(agentMsg.Content)
			if !agentMsg.Partial {
				fmt.Printf("\n--- Response ---\n%s\n", response.String())
			}
		}

		if msg.GetAgentDone() != nil {
			fmt.Println("--- Done ---")
			return nil
		}

		if errResp := msg.GetError(); errResp != nil {
			return fmt.Errorf("agent error: %s: %s", errResp.Error.Code, errResp.Error.Message)
		}

		// Also print events for debugging
		if event := msg.GetAgentEvent(); event != nil {
			fmt.Printf("[event] %s\n", event.Event.Kind)
		}
	}
}
