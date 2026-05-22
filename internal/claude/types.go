package claude

import "time"

type Status int

const (
	StatusUnknown Status = iota
	StatusIdle
	StatusBusy
	StatusNeedsInput
	StatusStopped
)

func (s Status) String() string {
	switch s {
	case StatusIdle:
		return "idle"
	case StatusBusy:
		return "busy"
	case StatusNeedsInput:
		return "needs-input"
	case StatusStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

type Origin int

const (
	OriginExternal Origin = iota
	OriginWhip
)

type Session struct {
	ID         string
	PID        int
	CWD        string
	Name       string
	Version    string
	Status     Status
	StartedAt  time.Time
	UpdatedAt  time.Time
	Origin     Origin
	TmuxTarget string
	Preview    string
}

// rawSession matches the on-disk shape of ~/.claude/sessions/<pid>.json.
type rawSession struct {
	PID         int    `json:"pid"`
	SessionID   string `json:"sessionId"`
	CWD         string `json:"cwd"`
	StartedAt   int64  `json:"startedAt"`
	UpdatedAt   int64  `json:"updatedAt"`
	Status      string `json:"status"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Kind        string `json:"kind"`
	Entrypoint  string `json:"entrypoint"`
	ProcStart   string `json:"procStart"`
}

// TranscriptEvent is a minimal projection of a transcript .jsonl line — only
// the fields the UI cares about for previews and status derivation.
type TranscriptEvent struct {
	Type      string
	Role      string
	Text      string
	ToolName  string
	IsError   bool
	Timestamp time.Time
}
