// Package tmuxctl shells out to tmux for spawning Claude sessions and
// injecting follow-up prompts. We use a dedicated tmux session named "whip"
// so windows we own are easy to find.
package tmuxctl

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const SessionName = "whip"

// Available reports whether tmux is installed.
func Available() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

// EnsureSession creates the "whip" tmux session if it does not exist. It
// starts detached so the user's foreground TUI is not disturbed.
func EnsureSession() error {
	if err := exec.Command("tmux", "has-session", "-t", SessionName).Run(); err == nil {
		return nil
	}
	cmd := exec.Command("tmux", "new-session", "-d", "-s", SessionName, "-n", "ready")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux new-session: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SpawnClaude opens a new tmux window in the whip session that starts a
// Claude session in cwd. Returns the window name.
func SpawnClaude(cwd, windowName string) (string, error) {
	if windowName == "" {
		windowName = "claude"
	}
	if err := EnsureSession(); err != nil {
		return "", err
	}
	target := SessionName + ":" + windowName
	cmd := exec.Command("tmux", "new-window", "-d", "-t", SessionName,
		"-n", windowName, "-c", cwd, "claude")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("tmux new-window: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return target, nil
}

// SendKeys submits text to a tmux target, then presses Enter. Used for
// follow-up prompts.
func SendKeys(target, text string) error {
	if target == "" {
		return errors.New("empty tmux target")
	}
	cmd := exec.Command("tmux", "send-keys", "-t", target, "-l", text)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux send-keys: %w: %s", err, strings.TrimSpace(string(out)))
	}
	cmd = exec.Command("tmux", "send-keys", "-t", target, "Enter")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux send-keys Enter: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
