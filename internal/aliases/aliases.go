// Package aliases stores user-assigned display names for sessions in a
// whip-owned sidecar file. We don't write into ~/.claude/sessions/*.json
// because claude overwrites those on every state flush.
package aliases

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type Store struct {
	path string

	mu  sync.RWMutex
	val map[string]string
}

// Load returns a Store backed by ~/.config/whip/aliases.json (or path arg).
// A missing file is fine — we'll create it on first save.
func Load(path string) (*Store, error) {
	if path == "" {
		dir, err := defaultDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(dir, "aliases.json")
	}
	s := &Store{path: path, val: map[string]string{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s.val); err != nil {
		return s, nil // tolerate corruption — start fresh rather than refusing to launch
	}
	return s, nil
}

func defaultDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "whip"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "whip"), nil
}

func (s *Store) Get(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.val[id]
}

// Set writes the alias to memory and persists the whole map. Empty value
// removes the entry.
func (s *Store) Set(id, name string) error {
	s.mu.Lock()
	if name == "" {
		delete(s.val, id)
	} else {
		s.val[id] = name
	}
	snapshot := make(map[string]string, len(s.val))
	for k, v := range s.val {
		snapshot[k] = v
	}
	s.mu.Unlock()
	return persist(s.path, snapshot)
}

func persist(path string, m map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
