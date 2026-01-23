package output

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRelativeTime(t *testing.T) {
	tests := []struct {
		name     string
		ts       *timestamppb.Timestamp
		expected string
	}{
		{
			name:     "nil timestamp",
			ts:       nil,
			expected: "-",
		},
		{
			name:     "just now",
			ts:       timestamppb.New(time.Now().Add(-30 * time.Second)),
			expected: "just now",
		},
		{
			name:     "minutes ago",
			ts:       timestamppb.New(time.Now().Add(-5 * time.Minute)),
			expected: "5m ago",
		},
		{
			name:     "hours ago",
			ts:       timestamppb.New(time.Now().Add(-3 * time.Hour)),
			expected: "3h ago",
		},
		{
			name:     "days ago",
			ts:       timestamppb.New(time.Now().Add(-48 * time.Hour)),
			expected: "2d ago",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RelativeTime(tt.ts)
			if result != tt.expected {
				t.Errorf("RelativeTime() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFormatTimestamp(t *testing.T) {
	tests := []struct {
		name     string
		ts       *timestamppb.Timestamp
		expected string
	}{
		{
			name:     "nil timestamp",
			ts:       nil,
			expected: "-",
		},
		{
			name:     "valid timestamp",
			ts:       timestamppb.New(time.Date(2024, 1, 15, 14, 30, 45, 0, time.UTC)),
			expected: time.Date(2024, 1, 15, 14, 30, 45, 0, time.UTC).Local().Format("15:04:05"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatTimestamp(tt.ts)
			if result != tt.expected {
				t.Errorf("FormatTimestamp() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "short string",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "exact length",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "truncate with ellipsis",
			input:    "hello world",
			maxLen:   8,
			expected: "hello...",
		},
		{
			name:     "very short max",
			input:    "hello",
			maxLen:   3,
			expected: "hel",
		},
		{
			name:     "empty string",
			input:    "",
			maxLen:   10,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Truncate(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestPadRight(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		width    int
		expected string
	}{
		{
			name:     "pad short string",
			input:    "hi",
			width:    5,
			expected: "hi   ",
		},
		{
			name:     "no padding needed",
			input:    "hello",
			width:    5,
			expected: "hello",
		},
		{
			name:     "string longer than width",
			input:    "hello world",
			width:    5,
			expected: "hello world",
		},
		{
			name:     "empty string",
			input:    "",
			width:    3,
			expected: "   ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PadRight(tt.input, tt.width)
			if result != tt.expected {
				t.Errorf("PadRight(%q, %d) = %q, want %q", tt.input, tt.width, result, tt.expected)
			}
		})
	}
}

func TestStatusColor(t *testing.T) {
	tests := []struct {
		status string
		notNil bool
	}{
		{"SESSION_STATUS_READY", true},
		{"SESSION_STATUS_RUNNING", true},
		{"SESSION_STATUS_PAUSED", true},
		{"SESSION_STATUS_ERROR", true},
		{"SESSION_STATUS_CREATING", true},
		{"SESSION_STATUS_RESUMING", true},
		{"unknown_status", true}, // Falls back to MutedColor
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			result := StatusColor(tt.status)
			if (result != nil) != tt.notNil {
				t.Errorf("StatusColor(%q) returned nil=%v, want nil=%v", tt.status, result == nil, !tt.notNil)
			}
		})
	}
}

func TestEventKindColor(t *testing.T) {
	tests := []struct {
		kind   string
		notNil bool
	}{
		{"tool_start", true},
		{"tool_end", true},
		{"file_change", true},
		{"command_start", true},
		{"command_end", true},
		{"thinking", true},
		{"port_exposed", true},
		{"repo_clone", true},
		{"unknown_kind", true}, // Falls back to MutedColor
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			result := EventKindColor(tt.kind)
			if (result != nil) != tt.notNil {
				t.Errorf("EventKindColor(%q) returned nil=%v, want nil=%v", tt.kind, result == nil, !tt.notNil)
			}
		})
	}
}

func TestJSON(t *testing.T) {
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	data := map[string]string{"key": "value"}
	err := JSON(data)

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("JSON() returned error: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	var result map[string]string
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if result["key"] != "value" {
		t.Errorf("expected key='value', got key='%s'", result["key"])
	}
}

func TestJSONLine(t *testing.T) {
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	data := map[string]int{"count": 42}
	err := JSONLine(data)

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("JSONLine() returned error: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Should be a single line
	if bytes.Count([]byte(output), []byte("\n")) != 1 {
		t.Errorf("JSONLine should produce exactly one line, got: %q", output)
	}

	var result map[string]int
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if result["count"] != 42 {
		t.Errorf("expected count=42, got count=%d", result["count"])
	}
}

// Benchmark tests

func BenchmarkTruncate(b *testing.B) {
	s := "This is a long string that needs to be truncated for display purposes"
	for i := 0; i < b.N; i++ {
		_ = Truncate(s, 30)
	}
}

func BenchmarkRelativeTime(b *testing.B) {
	ts := timestamppb.New(time.Now().Add(-2 * time.Hour))
	for i := 0; i < b.N; i++ {
		_ = RelativeTime(ts)
	}
}

func BenchmarkPadRight(b *testing.B) {
	s := "short"
	for i := 0; i < b.N; i++ {
		_ = PadRight(s, 20)
	}
}
