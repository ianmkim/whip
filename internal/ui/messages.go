package ui

import (
	"time"

	"github.com/adrian/whip/internal/claude"
	tea "github.com/charmbracelet/bubbletea"
)

// Messages flowing through Bubble Tea's Update loop. Async work (file
// watching, transcript reads, SFTP polls) sends one of these via
// Program.Send to keep the model immutable from goroutines.

type sessionsLoadedMsg struct {
	sessions []claude.Session
	err      error
}

type sessionUpsertMsg struct{ session claude.Session }
type sessionDeleteMsg struct{ id string }

// UpsertMsg / DeleteMsg are exported wrappers so main can forward Source
// events into the Bubble Tea program from a goroutine.
func UpsertMsg(s claude.Session) tea.Msg { return sessionUpsertMsg{session: s} }
func DeleteMsg(id string) tea.Msg        { return sessionDeleteMsg{id: id} }
type watchClosedMsg struct{}

type transcriptMsg struct {
	id     string
	events []claude.TranscriptEvent
	err    error
}

type tickMsg time.Time
type fadeTickMsg time.Time
type expandTickMsg time.Time
type transcriptTickMsg struct{ id string }
type replyAnimTickMsg struct{ id string }

// toastMsg shows a short status string at the bottom of the screen for ~3s.
type toastMsg struct{ text string }
type toastClearMsg struct{}

// chordTimeoutMsg fires when a pending key chord (e.g. "g") expires.
type chordTimeoutMsg struct{ generation int }

type attachExitedMsg struct{ err error }
