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

var promptWait bool

var promptCmd = &cobra.Command{
	Use:   "prompt <session-id> <text>",
	Short: "Send a prompt to a session (for testing)",
	Args:  cobra.ExactArgs(2),
	RunE:  runPrompt,
}

func init() {
	promptCmd.Flags().BoolVarP(&promptWait, "wait", "w", false, "Wait for and stream the response")
	rootCmd.AddCommand(promptCmd)
}

func runPrompt(cmd *cobra.Command, args []string) error {
	sessionID := args[0]
	text := args[1]

	// Use shorter timeout for non-wait mode
	timeout := 10 * time.Second
	if promptWait {
		timeout = 120 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	c := client.New(getServerURL())

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

	fmt.Printf("Prompt sent to session %s\n", sessionID)

	// If not waiting, just print how to check messages and exit
	if !promptWait {
		fmt.Printf("\nTo check messages:\n  netclode messages %s\n", sessionID)
		return nil
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
