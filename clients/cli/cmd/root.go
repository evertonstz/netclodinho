package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const (
	// DefaultURL is the local development URL.
	// For production, set NETCLODE_URL to your Tailscale ingress URL.
	DefaultURL = "http://localhost:3000"
)

var (
	serverURL  string
	jsonOutput bool
)

var rootCmd = &cobra.Command{
	Use:   "netclode",
	Short: "Netclode CLI - debug and inspect sessions",
	Long: `Netclode CLI connects to the control-plane to inspect sessions,
messages, and events for debugging purposes.

Set NETCLODE_URL environment variable or use --url flag to specify
the control-plane URL.`,
	SilenceUsage: true,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&serverURL, "url", "", "Control-plane URL (default: $NETCLODE_URL or production)")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	// Set default from environment
	cobra.OnInitialize(initConfig)
}

func initConfig() {
	if serverURL == "" {
		serverURL = os.Getenv("NETCLODE_URL")
	}
	if serverURL == "" {
		serverURL = DefaultURL
	}
}

func getServerURL() string {
	return serverURL
}

func isJSONOutput() bool {
	return jsonOutput
}

func exitError(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}
