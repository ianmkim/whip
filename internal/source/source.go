package source

import (
	"context"

	"github.com/adrian/whip/internal/claude"
)

type EventKind int

const (
	EventUpsert EventKind = iota
	EventDelete
)

// Event is what a Source.Watch loop emits. It is intentionally coarse —
// the consumer reconciles by ID.
type Event struct {
	Kind    EventKind
	Session claude.Session // valid for Upsert; for Delete only ID is set
}

// Source abstracts where Claude session state comes from. There are two
// implementations: a local one watching ~/.claude with fsnotify, and a
// remote one polling over SFTP.
type Source interface {
	// Label is shown in the UI header (e.g. "local" or "box").
	Label() string

	// List returns the current snapshot of all sessions. Used to seed the UI.
	List(ctx context.Context) ([]claude.Session, error)

	// Tail reads up to n recent transcript events for a session. n=0 means
	// "everything we'd plausibly want to display."
	Tail(ctx context.Context, sessionID string, n int) ([]claude.TranscriptEvent, error)

	// Watch streams session changes. The channel closes when ctx is cancelled.
	Watch(ctx context.Context) (<-chan Event, error)

	// Close releases any underlying resources (SSH connections, watchers).
	Close() error
}
