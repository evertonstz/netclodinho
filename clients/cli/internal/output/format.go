package output

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	// Colors for different elements
	HeaderColor  = color.New(color.FgWhite, color.Bold)
	IDColor      = color.New(color.FgCyan)
	NameColor    = color.New(color.FgWhite)
	TimeColor    = color.New(color.FgHiBlack)
	SuccessColor = color.New(color.FgGreen)
	WarningColor = color.New(color.FgYellow)
	ErrorColor   = color.New(color.FgRed)
	MutedColor   = color.New(color.FgHiBlack)

	// Status colors
	StatusColors = map[string]*color.Color{
		"creating": color.New(color.FgYellow),
		"resuming": color.New(color.FgYellow),
		"ready":    color.New(color.FgGreen),
		"running":  color.New(color.FgGreen, color.Bold),
		"paused":   color.New(color.FgHiBlack),
		"error":    color.New(color.FgRed),
	}

	// Event kind colors
	EventKindColors = map[string]*color.Color{
		"tool_start":          color.New(color.FgBlue),
		"tool_input":          color.New(color.FgHiBlack),
		"tool_input_complete": color.New(color.FgHiBlack),
		"tool_end":            color.New(color.FgBlue),
		"file_change":         color.New(color.FgGreen),
		"command_start":       color.New(color.FgMagenta),
		"command_end":         color.New(color.FgMagenta),
		"thinking":            color.New(color.FgHiBlack, color.Italic),
		"port_exposed":        color.New(color.FgCyan),
		"port_unexposed":      color.New(color.FgYellow),
		"repo_clone":          color.New(color.FgYellow),
	}
)

// JSON outputs the value as JSON.
func JSON(v any) error {
	// Check if it's a proto message
	if pm, ok := v.(proto.Message); ok {
		data, err := protojson.MarshalOptions{
			EmitUnpopulated: false,
			Indent:          "  ",
		}.Marshal(pm)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// JSONLine outputs the value as a single line of JSON (for streaming).
func JSONLine(v any) error {
	if pm, ok := v.(proto.Message); ok {
		data, err := protojson.MarshalOptions{
			EmitUnpopulated: false,
		}.Marshal(pm)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(v)
}

// RelativeTime formats a timestamp as relative time (e.g., "2h ago").
func RelativeTime(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return "-"
	}
	t := ts.AsTime()
	if t.IsZero() {
		return "-"
	}

	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// FormatTimestamp formats a timestamp for display.
func FormatTimestamp(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return "-"
	}
	return ts.AsTime().Local().Format("15:04:05")
}

// StatusColor returns the appropriate color for a session status.
func StatusColor(status string) *color.Color {
	// Convert proto enum to string (e.g., "SESSION_STATUS_READY" -> "ready")
	s := strings.ToLower(status)
	s = strings.TrimPrefix(s, "session_status_")

	if c, ok := StatusColors[s]; ok {
		return c
	}
	return MutedColor
}

// EventKindColor returns the appropriate color for an event kind.
func EventKindColor(kind string) *color.Color {
	if c, ok := EventKindColors[kind]; ok {
		return c
	}
	return MutedColor
}

// Truncate truncates a string to the specified length.
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// PadRight pads a string to the right with spaces.
func PadRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
