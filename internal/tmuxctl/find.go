package tmuxctl

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// FindPaneByPID walks the ancestor chain of pid looking for a tmux pane that
// contains it. Returns the target string ("session:window.pane") or empty if
// the process isn't running under tmux. Only inspects local /proc, so use the
// remote variant for SSH'd sessions.
func FindPaneByPID(pid int) (string, error) {
	panes, err := listLocalPanes()
	if err != nil {
		return "", err
	}
	if len(panes) == 0 {
		return "", nil
	}
	cur := pid
	for i := 0; i < 32 && cur > 1; i++ {
		if target, ok := panes[cur]; ok {
			return target, nil
		}
		ppid, err := localParentPID(cur)
		if err != nil || ppid <= 1 {
			break
		}
		cur = ppid
	}
	return "", nil
}

func listLocalPanes() (map[int]string, error) {
	out, err := exec.Command("tmux", "list-panes", "-a", "-F",
		"#{pane_pid} #{session_name}:#{window_index}.#{pane_index}").Output()
	if err != nil {
		// tmux not running or no server — treat as "no panes", not a hard error.
		return nil, nil
	}
	return parsePanes(string(out)), nil
}

func parsePanes(s string) map[int]string {
	m := map[int]string{}
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		fields := strings.SplitN(line, " ", 2)
		if len(fields) != 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		m[pid] = fields[1]
	}
	return m
}

func localParentPID(pid int) (int, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	// /proc/<pid>/stat: "pid (comm) state ppid ...". `comm` may contain
	// spaces and parens, so we anchor on the last ')'.
	s := string(data)
	end := strings.LastIndex(s, ")")
	if end < 0 || end+2 >= len(s) {
		return 0, fmt.Errorf("malformed stat for %d", pid)
	}
	fields := strings.Fields(s[end+2:])
	if len(fields) < 2 {
		return 0, fmt.Errorf("missing ppid for %d", pid)
	}
	return strconv.Atoi(fields[1])
}

// RemoteTmuxLookupScript is a small POSIX shell program that, given a pid as
// $1, prints the containing tmux target (e.g. "work:0.0") or nothing. We pipe
// it over SSH for remote sources. Kept here so the attach code can inline it.
const RemoteTmuxLookupScript = `
pid="$1"
panes=$(tmux list-panes -a -F '#{pane_pid} #{session_name}:#{window_index}.#{pane_index}' 2>/dev/null) || exit 0
cur=$pid
i=0
while [ "$cur" -gt 1 ] && [ "$i" -lt 32 ]; do
  match=$(printf '%s\n' "$panes" | awk -v p="$cur" '$1==p {print $2; exit}')
  if [ -n "$match" ]; then printf '%s' "$match"; exit 0; fi
  next=$(ps -o ppid= -p "$cur" 2>/dev/null | tr -d ' ')
  [ -z "$next" ] && break
  cur=$next
  i=$((i+1))
done
`
