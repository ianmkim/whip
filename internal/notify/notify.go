// Package notify wraps gen2brain/beeep with per-session debouncing so that
// status flapping does not spam the desktop.
package notify

import (
	"os/exec"
	"sync"
	"time"

	"github.com/gen2brain/beeep"
)

const debounce = 5 * time.Second

type Notifier struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func New() *Notifier {
	return &Notifier{last: map[string]time.Time{}}
}

// Send pushes a desktop notification, suppressing repeats per key inside the
// debounce window. The key should typically be sessionID+status so different
// transitions do not block each other.
func (n *Notifier) Send(key, title, body string) {
	now := time.Now()
	n.mu.Lock()
	if t, ok := n.last[key]; ok && now.Sub(t) < debounce {
		n.mu.Unlock()
		return
	}
	n.last[key] = now
	n.mu.Unlock()

	if err := beeep.Notify(title, body, ""); err == nil {
		return
	}
	// Fallback: many Linux desktops have notify-send available even when
	// beeep's DBus path fails (e.g. minimal sessions without WAYLAND_DISPLAY).
	if path, err := exec.LookPath("notify-send"); err == nil {
		_ = exec.Command(path, title, body).Run()
	}
}
