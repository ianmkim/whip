package claude

import (
	"strings"
	"testing"
)

func TestParseSession(t *testing.T) {
	const blob = `{"pid":807564,"sessionId":"bb2dc8ba-219b-440a-8158-c09023a03b52","cwd":"/home/adrian/src/whip","startedAt":1779407687030,"procStart":"16178410","version":"2.1.147","peerProtocol":1,"kind":"interactive","entrypoint":"cli","status":"busy","updatedAt":1779410679639,"name":"whip"}`
	s, err := ParseSession([]byte(blob))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.ID != "bb2dc8ba-219b-440a-8158-c09023a03b52" {
		t.Errorf("id: %q", s.ID)
	}
	if s.PID != 807564 {
		t.Errorf("pid: %d", s.PID)
	}
	if s.Status != StatusBusy {
		t.Errorf("status: %v", s.Status)
	}
	if s.CWD != "/home/adrian/src/whip" {
		t.Errorf("cwd: %q", s.CWD)
	}
	if s.UpdatedAt.IsZero() {
		t.Errorf("updatedAt zero")
	}
}

func TestParseSessionMissingID(t *testing.T) {
	if _, err := ParseSession([]byte(`{"pid":1}`)); err == nil {
		t.Fatalf("expected error")
	}
}

func TestParseTranscript(t *testing.T) {
	const stream = `{"type":"permission-mode","permissionMode":"plan","sessionId":"x"}
{"type":"file-history-snapshot","messageId":"y","snapshot":{},"isSnapshotUpdate":false}
{"type":"user","message":{"role":"user","content":"hello world"},"uuid":"u1","timestamp":"2026-05-21T23:57:16.164Z","sessionId":"x"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"sure"},{"type":"tool_use","id":"t1","name":"Bash","input":{}}]},"uuid":"u2","timestamp":"2026-05-21T23:57:17.000Z","sessionId":"x"}
{"type":"user","isSidechain":true,"message":{"role":"user","content":"ignored"},"timestamp":"2026-05-21T23:57:18.000Z"}
`
	events, err := ParseTranscript(strings.NewReader(stream), 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(events), events)
	}
	if events[0].Text != "hello world" {
		t.Errorf("first text: %q", events[0].Text)
	}
	if events[1].Type != "assistant" || events[1].Text != "sure" {
		t.Errorf("second: %+v", events[1])
	}
	if events[2].Type != "tool_use" || events[2].ToolName != "Bash" {
		t.Errorf("third: %+v", events[2])
	}
	if got := Preview(events); got != "↳ Bash" {
		t.Errorf("preview: %q", got)
	}
}
