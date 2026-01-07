package protocol

type AgentEventKind string

const (
	EventKindToolStart    AgentEventKind = "tool_start"
	EventKindToolInput    AgentEventKind = "tool_input"
	EventKindToolEnd      AgentEventKind = "tool_end"
	EventKindFileChange   AgentEventKind = "file_change"
	EventKindCommandStart AgentEventKind = "command_start"
	EventKindCommandEnd   AgentEventKind = "command_end"
	EventKindThinking     AgentEventKind = "thinking"
	EventKindPortDetected AgentEventKind = "port_detected"
)

// AgentEvent is a polymorphic event type. Fields are optional based on Kind.
type AgentEvent struct {
	Kind       AgentEventKind         `json:"kind"`
	Timestamp  string                 `json:"timestamp"`
	Tool       string                 `json:"tool,omitempty"`
	ToolUseID  string                 `json:"toolUseId,omitempty"`
	Input      map[string]interface{} `json:"input,omitempty"`
	InputDelta string                 `json:"inputDelta,omitempty"`
	Result     *string                `json:"result,omitempty"`
	Error      *string                `json:"error,omitempty"`
	Path      string                 `json:"path,omitempty"`
	Action    string                 `json:"action,omitempty"`
	Command   string                 `json:"command,omitempty"`
	Cwd       *string                `json:"cwd,omitempty"`
	ExitCode  *int                   `json:"exitCode,omitempty"`
	Output    *string                `json:"output,omitempty"`
	Content   string                 `json:"content,omitempty"`
	Port      int                    `json:"port,omitempty"`
	Process   *string                `json:"process,omitempty"`
	PreviewURL *string               `json:"previewUrl,omitempty"`
	LinesAdded   *int                `json:"linesAdded,omitempty"`
	LinesRemoved *int                `json:"linesRemoved,omitempty"`
}
