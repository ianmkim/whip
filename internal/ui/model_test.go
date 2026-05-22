package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/adrian/whip/internal/claude"
	"github.com/adrian/whip/internal/source"
	tea "github.com/charmbracelet/bubbletea"
)

// stubSource implements source.Source with canned data for UI tests.
type stubSource struct {
	sessions []claude.Session
}

func (s *stubSource) Label() string                                       { return "stub" }
func (s *stubSource) List(context.Context) ([]claude.Session, error)      { return s.sessions, nil }
func (s *stubSource) Tail(context.Context, string, int) ([]claude.TranscriptEvent, error) {
	return nil, nil
}
func (s *stubSource) Watch(ctx context.Context) (<-chan source.Event, error) {
	ch := make(chan source.Event)
	go func() { <-ctx.Done(); close(ch) }()
	return ch, nil
}
func (s *stubSource) Close() error { return nil }

func TestViewWithSessions(t *testing.T) {
	src := &stubSource{sessions: []claude.Session{
		{ID: "a", CWD: "/x/y/proj1", Status: claude.StatusBusy, UpdatedAt: time.Now()},
		{ID: "b", CWD: "/x/y/proj2", Status: claude.StatusIdle, UpdatedAt: time.Now().Add(-time.Hour)},
	}}
	m := NewModel(src, nil, nil, false, "local")
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	upd, _ = upd.Update(sessionsLoadedMsg{sessions: src.sessions})
	out := upd.View()
	if !strings.Contains(out, "whip") {
		t.Errorf("missing title in output")
	}
	if !strings.Contains(out, "proj1") || !strings.Contains(out, "proj2") {
		t.Errorf("missing cwd basenames in output:\n%s", out)
	}
}

func TestVimChordGG(t *testing.T) {
	src := &stubSource{sessions: []claude.Session{
		{ID: "a", CWD: "/x/y/p1", Status: claude.StatusIdle, UpdatedAt: time.Now()},
		{ID: "b", CWD: "/x/y/p2", Status: claude.StatusIdle, UpdatedAt: time.Now().Add(-time.Minute)},
		{ID: "c", CWD: "/x/y/p3", Status: claude.StatusIdle, UpdatedAt: time.Now().Add(-time.Hour)},
	}}
	m := NewModel(src, nil, nil, false, "local")
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	upd, _ = upd.Update(sessionsLoadedMsg{sessions: src.sessions})
	mm := upd.(Model)
	mm.selected = "c"
	// press g g — should jump to top
	upd2, _ := mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	upd2, _ = upd2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	got := upd2.(Model).selected
	if got != "a" {
		t.Errorf("after gg expected selected=a, got %q", got)
	}
}
