package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/angristan/netclode/clients/cli/internal/client"
	"github.com/angristan/netclode/clients/cli/internal/output"
	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/spf13/cobra"
)

var sessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "Manage sessions",
	Long:  "List, inspect, and manage Netclode sessions.",
}

var sessionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all sessions",
	RunE:  runSessionsList,
}

var sessionsGetCmd = &cobra.Command{
	Use:   "get <session-id>",
	Short: "Get session details",
	Args:  cobra.ExactArgs(1),
	RunE:  runSessionsGet,
}

var sessionsDeleteCmd = &cobra.Command{
	Use:   "delete <session-id>",
	Short: "Delete a session",
	Args:  cobra.ExactArgs(1),
	RunE:  runSessionsDelete,
}

var sessionsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new session",
	RunE:  runSessionsCreate,
}

var (
	createName    string
	createRepo    string
	createSdkType string
	createModel   string
)

func init() {
	rootCmd.AddCommand(sessionsCmd)
	sessionsCmd.AddCommand(sessionsListCmd)
	sessionsCmd.AddCommand(sessionsGetCmd)
	sessionsCmd.AddCommand(sessionsDeleteCmd)
	sessionsCmd.AddCommand(sessionsCreateCmd)

	sessionsCreateCmd.Flags().StringVar(&createName, "name", "", "Session name")
	sessionsCreateCmd.Flags().StringVar(&createRepo, "repo", "", "GitHub repository (owner/repo)")
	sessionsCreateCmd.Flags().StringVar(&createSdkType, "sdk", "claude", "SDK type (claude or opencode)")
	sessionsCreateCmd.Flags().StringVar(&createModel, "model", "", "Model ID for OpenCode (e.g., anthropic/claude-sonnet-4-0)")
}

func runSessionsList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c := client.New(getServerURL())

	sessions, err := c.SyncSessions(ctx)
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}

	if isJSONOutput() {
		return output.JSON(sessions)
	}

	if len(sessions) == 0 {
		fmt.Println("No sessions found.")
		return nil
	}

	printSessionsTable(sessions)
	return nil
}

func printSessionsTable(sessions []*pb.SessionSummary) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	// Header
	_, _ = output.HeaderColor.Fprintf(w, "ID\tNAME\tSTATUS\tREPO\tMSGS\tCREATED\tACTIVE\n")

	for _, s := range sessions {
		sess := s.Session
		id := output.Truncate(sess.Id, 16)
		name := output.Truncate(sess.Name, 30)
		status := formatStatus(sess.Status.String())
		repo := "-"
		if sess.Repo != nil {
			repo = output.Truncate(*sess.Repo, 20)
		}
		msgs := "-"
		if s.MessageCount != nil {
			msgs = fmt.Sprintf("%d", *s.MessageCount)
		}
		created := output.RelativeTime(sess.CreatedAt)
		active := output.RelativeTime(sess.LastActiveAt)

		statusColor := output.StatusColor(sess.Status.String())

		_, _ = output.IDColor.Fprintf(w, "%s\t", id)
		_, _ = output.NameColor.Fprintf(w, "%s\t", name)
		_, _ = statusColor.Fprintf(w, "%s\t", status)
		_, _ = fmt.Fprintf(w, "%s\t", repo)
		_, _ = fmt.Fprintf(w, "%s\t", msgs)
		_, _ = output.TimeColor.Fprintf(w, "%s\t", created)
		_, _ = output.TimeColor.Fprintf(w, "%s\n", active)
	}

	_ = w.Flush()
}

func formatStatus(status string) string {
	// Convert "SESSION_STATUS_READY" -> "ready"
	s := strings.ToLower(status)
	s = strings.TrimPrefix(s, "session_status_")
	return s
}

func runSessionsGet(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c := client.New(getServerURL())
	sessionID := args[0]

	state, err := c.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}

	if isJSONOutput() {
		return output.JSON(map[string]interface{}{
			"session":      state.Session,
			"messageCount": len(state.Messages),
			"eventCount":   len(state.Events),
			"hasMore":      state.HasMore,
		})
	}

	printSessionDetails(state)
	return nil
}

func printSessionDetails(state *client.SessionState) {
	sess := state.Session

	fmt.Println()
	_, _ = output.HeaderColor.Println("Session Details")
	fmt.Println(strings.Repeat("-", 40))

	fmt.Printf("%-15s ", "ID:")
	_, _ = output.IDColor.Println(sess.Id)

	fmt.Printf("%-15s %s\n", "Name:", sess.Name)

	status := formatStatus(sess.Status.String())
	fmt.Printf("%-15s ", "Status:")
	_, _ = output.StatusColor(sess.Status.String()).Println(status)

	repo := "-"
	if sess.Repo != nil {
		repo = *sess.Repo
	}
	fmt.Printf("%-15s %s\n", "Repo:", repo)

	fmt.Printf("%-15s %s (%s)\n", "Created:",
		output.FormatTimestamp(sess.CreatedAt),
		output.RelativeTime(sess.CreatedAt))

	fmt.Printf("%-15s %s (%s)\n", "Last Active:",
		output.FormatTimestamp(sess.LastActiveAt),
		output.RelativeTime(sess.LastActiveAt))

	fmt.Println()
	_, _ = output.HeaderColor.Println("Statistics")
	fmt.Println(strings.Repeat("-", 40))
	fmt.Printf("%-15s %d\n", "Messages:", len(state.Messages))
	fmt.Printf("%-15s %d\n", "Events:", len(state.Events))
	if state.HasMore {
		_, _ = output.MutedColor.Println("(more messages available)")
	}
	fmt.Println()
}

func runSessionsDelete(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c := client.New(getServerURL())
	sessionID := args[0]

	if err := c.DeleteSession(ctx, sessionID); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	if isJSONOutput() {
		return output.JSON(map[string]string{
			"deleted": sessionID,
		})
	}

	_, _ = output.SuccessColor.Printf("Deleted session %s\n", sessionID)
	return nil
}

func runSessionsCreate(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c := client.New(getServerURL())

	// Parse SDK type
	var sdkType pb.SdkType
	switch strings.ToLower(createSdkType) {
	case "opencode":
		sdkType = pb.SdkType_SDK_TYPE_OPENCODE
	case "claude", "":
		sdkType = pb.SdkType_SDK_TYPE_CLAUDE
	default:
		return fmt.Errorf("invalid SDK type: %s (use 'claude' or 'opencode')", createSdkType)
	}

	opts := client.CreateSessionOptions{
		Name:    createName,
		Repo:    createRepo,
		SdkType: sdkType,
		Model:   createModel,
	}

	session, err := c.CreateSession(ctx, opts)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	if isJSONOutput() {
		return output.JSON(session)
	}

	_, _ = output.SuccessColor.Printf("Created session %s\n", session.Id)
	fmt.Printf("  Name:     %s\n", session.Name)
	fmt.Printf("  Status:   %s\n", formatStatus(session.Status.String()))
	if session.SdkType != nil {
		fmt.Printf("  SDK:      %s\n", formatSdkType(*session.SdkType))
	}
	if session.Model != nil {
		fmt.Printf("  Model:    %s\n", *session.Model)
	}

	return nil
}

func formatSdkType(sdkType pb.SdkType) string {
	switch sdkType {
	case pb.SdkType_SDK_TYPE_OPENCODE:
		return "opencode"
	case pb.SdkType_SDK_TYPE_CLAUDE:
		return "claude"
	default:
		return "unknown"
	}
}
