package source

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/adrian/whip/internal/claude"
	"github.com/adrian/whip/internal/sshx"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Remote talks to ~/.claude on a host reachable via ~/.ssh/config. SFTP has
// no inotify equivalent, so we poll. The poll is light — one stat per session
// file plus one ReadDir on sessions/ — and runs once per second.
type Remote struct {
	alias   string
	client  *ssh.Client
	sftp    *sftp.Client
	homeDir string

	pollInterval time.Duration

	mu      sync.Mutex
	known   map[string]knownEntry // sessionId -> last seen
}

type knownEntry struct {
	session claude.Session
	size    int64
	mtime   time.Time
}

func NewRemote(alias string) (*Remote, error) {
	client, err := sshx.Dial(alias)
	if err != nil {
		return nil, err
	}
	sc, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("sftp: %w", err)
	}
	home, err := remoteHome(client)
	if err != nil {
		sc.Close()
		client.Close()
		return nil, err
	}
	return &Remote{
		alias:        alias,
		client:       client,
		sftp:         sc,
		homeDir:      home,
		pollInterval: time.Second,
		known:        map[string]knownEntry{},
	}, nil
}

func (r *Remote) Label() string { return r.alias }

func (r *Remote) sessionsDir() string { return path.Join(r.homeDir, ".claude", "sessions") }
func (r *Remote) projectsDir() string { return path.Join(r.homeDir, ".claude", "projects") }

func (r *Remote) List(ctx context.Context) ([]claude.Session, error) {
	entries, err := r.sftp.ReadDir(r.sessionsDir())
	if err != nil {
		return nil, fmt.Errorf("readdir %s: %w", r.sessionsDir(), err)
	}
	var out []claude.Session
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		full := path.Join(r.sessionsDir(), e.Name())
		s, err := r.readSession(full)
		if err != nil {
			continue
		}
		r.mu.Lock()
		r.known[s.ID] = knownEntry{session: s, size: e.Size(), mtime: e.ModTime()}
		r.mu.Unlock()
		out = append(out, s)
	}
	return out, nil
}

func (r *Remote) Tail(_ context.Context, sessionID string, n int) ([]claude.TranscriptEvent, error) {
	p, err := r.findTranscriptPath(sessionID)
	if err != nil || p == "" {
		return nil, err
	}
	f, err := r.sftp.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return claude.ParseTranscript(f, n)
}

func (r *Remote) findTranscriptPath(sessionID string) (string, error) {
	dirs, err := r.sftp.ReadDir(r.projectsDir())
	if err != nil {
		return "", err
	}
	want := sessionID + ".jsonl"
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		candidate := path.Join(r.projectsDir(), d.Name(), want)
		if _, err := r.sftp.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", nil
}

func (r *Remote) Watch(ctx context.Context) (<-chan Event, error) {
	out := make(chan Event, 16)
	go r.poll(ctx, out)
	return out, nil
}

func (r *Remote) poll(ctx context.Context, out chan<- Event) {
	defer close(out)
	t := time.NewTicker(r.pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		r.scanOnce(ctx, out)
	}
}

func (r *Remote) scanOnce(ctx context.Context, out chan<- Event) {
	entries, err := r.sftp.ReadDir(r.sessionsDir())
	if err != nil {
		return
	}
	seen := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		full := path.Join(r.sessionsDir(), e.Name())
		size, mtime := e.Size(), e.ModTime()
		// Cheap change detection: skip if size+mtime identical to last seen.
		// We have to resolve to a sessionId to dedupe properly, so compare
		// after read for correctness.
		s, err := r.readSession(full)
		if err != nil {
			continue
		}
		seen[s.ID] = struct{}{}
		r.mu.Lock()
		prev, had := r.known[s.ID]
		r.known[s.ID] = knownEntry{session: s, size: size, mtime: mtime}
		r.mu.Unlock()
		if had && prev.size == size && prev.mtime.Equal(mtime) && prev.session.Status == s.Status {
			continue
		}
		send(ctx, out, Event{Kind: EventUpsert, Session: s})
	}
	// Detect deletes.
	r.mu.Lock()
	var deletes []claude.Session
	for id, k := range r.known {
		if _, ok := seen[id]; !ok {
			deletes = append(deletes, k.session)
			delete(r.known, id)
		}
	}
	r.mu.Unlock()
	for _, d := range deletes {
		send(ctx, out, Event{Kind: EventDelete, Session: claude.Session{ID: d.ID}})
	}
}

func (r *Remote) readSession(p string) (claude.Session, error) {
	f, err := r.sftp.Open(p)
	if err != nil {
		return claude.Session{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return claude.Session{}, err
	}
	return claude.ParseSession(data)
}

func (r *Remote) Close() error {
	if r.sftp != nil {
		r.sftp.Close()
	}
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}

// remoteHome runs `echo $HOME` on the remote to resolve the user's home dir.
func remoteHome(c *ssh.Client) (string, error) {
	sess, err := c.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	out, err := sess.Output("printf %s \"$HOME\"")
	if err != nil {
		return "", fmt.Errorf("resolve $HOME: %w", err)
	}
	home := strings.TrimSpace(string(out))
	if home == "" {
		return "", fmt.Errorf("empty $HOME")
	}
	return home, nil
}
