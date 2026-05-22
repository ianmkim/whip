package source

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/adrian/whip/internal/claude"
	"github.com/fsnotify/fsnotify"
)

// Local reads Claude state from a ~/.claude directory on the local filesystem
// and uses fsnotify to push changes to the UI in real time.
type Local struct {
	root string // typically ~/.claude

	mu      sync.Mutex
	known   map[string]claude.Session // path -> last-seen session
	watcher *fsnotify.Watcher
}

func NewLocal(root string) (*Local, error) {
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		root = filepath.Join(home, ".claude")
	}
	return &Local{root: root, known: map[string]claude.Session{}}, nil
}

func (l *Local) Label() string { return "local" }

func (l *Local) sessionsDir() string { return filepath.Join(l.root, "sessions") }
func (l *Local) projectsDir() string { return filepath.Join(l.root, "projects") }

func (l *Local) List(ctx context.Context) ([]claude.Session, error) {
	entries, err := os.ReadDir(l.sessionsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []claude.Session
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(l.sessionsDir(), e.Name())
		s, err := readSessionFile(path)
		if err != nil {
			continue // skip malformed; they tend to be transient
		}
		l.mu.Lock()
		l.known[path] = s
		l.mu.Unlock()
		out = append(out, s)
	}
	return out, nil
}

func (l *Local) Tail(_ context.Context, sessionID string, n int) ([]claude.TranscriptEvent, error) {
	path, err := l.findTranscriptPath(sessionID)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return claude.ParseTranscript(f, n)
}

// findTranscriptPath walks ~/.claude/projects/*/<sessionID>.jsonl. Faster than
// it sounds — directory count is bounded by number of project paths the user
// has ever opened in Claude.
func (l *Local) findTranscriptPath(sessionID string) (string, error) {
	entries, err := os.ReadDir(l.projectsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	want := sessionID + ".jsonl"
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(l.projectsDir(), e.Name(), want)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", nil
}

func (l *Local) Watch(ctx context.Context) (<-chan Event, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(l.sessionsDir(), 0o755); err != nil {
		w.Close()
		return nil, err
	}
	if err := w.Add(l.sessionsDir()); err != nil {
		w.Close()
		return nil, fmt.Errorf("watch %s: %w", l.sessionsDir(), err)
	}
	l.watcher = w
	out := make(chan Event, 16)
	go l.run(ctx, w, out)
	return out, nil
}

func (l *Local) run(ctx context.Context, w *fsnotify.Watcher, out chan<- Event) {
	defer close(out)
	defer w.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if !strings.HasSuffix(ev.Name, ".json") {
				continue
			}
			if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				l.mu.Lock()
				prev, had := l.known[ev.Name]
				delete(l.known, ev.Name)
				l.mu.Unlock()
				if had {
					send(ctx, out, Event{Kind: EventDelete, Session: claude.Session{ID: prev.ID}})
				}
				continue
			}
			s, err := readSessionFile(ev.Name)
			if err != nil {
				continue
			}
			l.mu.Lock()
			l.known[ev.Name] = s
			l.mu.Unlock()
			send(ctx, out, Event{Kind: EventUpsert, Session: s})
		case _, ok := <-w.Errors:
			if !ok {
				return
			}
		}
	}
}

func send(ctx context.Context, ch chan<- Event, ev Event) {
	select {
	case ch <- ev:
	case <-ctx.Done():
	}
}

func (l *Local) Close() error {
	if l.watcher != nil {
		return l.watcher.Close()
	}
	return nil
}

func readSessionFile(path string) (claude.Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return claude.Session{}, err
	}
	return claude.ParseSession(data)
}
