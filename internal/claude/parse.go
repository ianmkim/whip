package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// ParseSession decodes a ~/.claude/sessions/<pid>.json blob into a Session.
// The on-disk Status field is mapped to our enum; transcript-derived signals
// (NeedsInput, Stopped) must be applied separately by the caller.
func ParseSession(data []byte) (Session, error) {
	var raw rawSession
	if err := json.Unmarshal(data, &raw); err != nil {
		return Session{}, fmt.Errorf("decode session: %w", err)
	}
	if raw.SessionID == "" {
		return Session{}, fmt.Errorf("session missing sessionId")
	}
	s := Session{
		ID:        raw.SessionID,
		PID:       raw.PID,
		CWD:       raw.CWD,
		Name:      raw.Name,
		Version:   raw.Version,
		StartedAt: msToTime(raw.StartedAt),
		UpdatedAt: msToTime(raw.UpdatedAt),
	}
	switch strings.ToLower(raw.Status) {
	case "":
		s.Status = StatusUnknown
	case "idle":
		s.Status = StatusIdle
	case "busy":
		s.Status = StatusBusy
	default:
		// Claude writes a third status while a tool-use permission prompt is
		// pending (e.g. "input_required" / "waiting"). Anything that isn't
		// idle, busy, or empty means the agent is alive but stalled on the
		// user — surface it as NeedsInput so the row stays visible.
		s.Status = StatusNeedsInput
	}
	return s, nil
}

func msToTime(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// transcriptLine is intentionally permissive — Claude writes several event
// shapes to the same JSONL stream, and we only need a few fields out of any.
type transcriptLine struct {
	Type        string          `json:"type"`
	Timestamp   string          `json:"timestamp"`
	Message     json.RawMessage `json:"message"`
	IsSidechain bool            `json:"isSidechain"`
}

type transcriptMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Name    string `json:"name"`
	IsError bool   `json:"is_error"`
	Content json.RawMessage `json:"content"`
}

// ParseTranscript reads a .jsonl transcript stream and returns the last n
// "interesting" events (user messages, assistant text, tool use/results).
// It skips sidechain events and bookkeeping types like file-history-snapshot.
func ParseTranscript(r io.Reader, n int) ([]TranscriptEvent, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var events []TranscriptEvent
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var tl transcriptLine
		if err := json.Unmarshal(line, &tl); err != nil {
			continue
		}
		if tl.IsSidechain {
			continue
		}
		switch tl.Type {
		case "user", "assistant":
		default:
			continue
		}
		ev := decodeMessageEvents(tl)
		events = append(events, ev...)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if n > 0 && len(events) > n {
		events = events[len(events)-n:]
	}
	return events, nil
}

func decodeMessageEvents(tl transcriptLine) []TranscriptEvent {
	var msg transcriptMessage
	if err := json.Unmarshal(tl.Message, &msg); err != nil {
		return nil
	}
	ts, _ := time.Parse(time.RFC3339Nano, tl.Timestamp)

	// Content can be either a plain string (older user messages) or an array
	// of typed blocks. Try string first.
	var asString string
	if err := json.Unmarshal(msg.Content, &asString); err == nil {
		return []TranscriptEvent{{
			Type: tl.Type, Role: msg.Role, Text: asString, Timestamp: ts,
		}}
	}
	var blocks []contentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return nil
	}
	var out []TranscriptEvent
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) == "" {
				continue
			}
			out = append(out, TranscriptEvent{
				Type: tl.Type, Role: msg.Role, Text: b.Text, Timestamp: ts,
			})
		case "tool_use":
			out = append(out, TranscriptEvent{
				Type: "tool_use", Role: msg.Role, ToolName: b.Name, Timestamp: ts,
			})
		case "tool_result":
			text := ""
			_ = json.Unmarshal(b.Content, &text) // best-effort
			out = append(out, TranscriptEvent{
				Type: "tool_result", Role: msg.Role, Text: text,
				IsError: b.IsError, Timestamp: ts,
			})
		}
	}
	return out
}

// Preview returns a short single-line summary of the most recent meaningful
// transcript event — used in the list row.
func Preview(events []TranscriptEvent) string {
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Type == "tool_use" {
			return "↳ " + e.ToolName
		}
		if e.Text != "" {
			return firstLine(e.Text)
		}
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:117] + "…"
	}
	return s
}
