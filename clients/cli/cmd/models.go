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

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "List available models",
	Long:  "List available AI models for a given SDK type.",
	RunE:  runModels,
}

var (
	modelsSdkType string
)

func init() {
	rootCmd.AddCommand(modelsCmd)
	modelsCmd.Flags().StringVar(&modelsSdkType, "sdk", "opencode", "SDK type (claude, opencode, copilot, or codex)")
}

func runModels(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	c := client.New(getServerURL())

	// Parse SDK type
	var sdkType pb.SdkType
	switch strings.ToLower(modelsSdkType) {
	case "opencode":
		sdkType = pb.SdkType_SDK_TYPE_OPENCODE
	case "copilot":
		sdkType = pb.SdkType_SDK_TYPE_COPILOT
	case "codex":
		sdkType = pb.SdkType_SDK_TYPE_CODEX
	case "claude", "":
		sdkType = pb.SdkType_SDK_TYPE_CLAUDE
	default:
		return fmt.Errorf("invalid SDK type: %s (use 'claude', 'opencode', 'copilot', or 'codex')", modelsSdkType)
	}

	models, err := c.ListModels(ctx, sdkType, nil)
	if err != nil {
		return fmt.Errorf("list models: %w", err)
	}

	if isJSONOutput() {
		return output.JSON(models)
	}

	if len(models) == 0 {
		fmt.Println("No models available.")
		return nil
	}

	printModelsTable(models)
	return nil
}

func printModelsTable(models []*pb.ModelInfo) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	// Header
	_, _ = output.HeaderColor.Fprintf(w, "ID\tNAME\tPROVIDER\tCAPABILITIES\n")

	for _, m := range models {
		id := m.Id
		name := m.Name
		provider := "-"
		if m.Provider != nil {
			provider = *m.Provider
		}
		caps := "-"
		if len(m.Capabilities) > 0 {
			caps = strings.Join(m.Capabilities, ", ")
		}

		_, _ = output.IDColor.Fprintf(w, "%s\t", id)
		_, _ = output.NameColor.Fprintf(w, "%s\t", name)
		_, _ = fmt.Fprintf(w, "%s\t", provider)
		_, _ = output.MutedColor.Fprintf(w, "%s\n", caps)
	}

	_ = w.Flush()
}
