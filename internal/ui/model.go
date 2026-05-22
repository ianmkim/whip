package ui

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/adrian/whip/internal/claude"
	"github.com/adrian/whip/internal/notify"
	"github.com/adrian/whip/internal/source"
	"github.com/adrian/whip/internal/tmuxctl"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// modalKind enumerates the secondary input states. Vim chord parsing is
// separate and only applies in modalNone.
type modalKind int

const (
	modalNone modalKind = iota
	modalFilter
	modalCmd
	modalFollowup
	modalHelp
)

type Model struct {
	src      source.Source
	notifier *notify.Notifier
	remote   bool // true when source is an SSH host
	host     string

	keys KeyMap
	help bool

	width, height int

	sessions []claude.Session
	selected string // sessionId; survives reorders better than index
	scroll   int    // index of first visible row, for j/k scrolling

	previews     map[string]string                  // sessionId -> short last-line summary
	previewKey   map[string]time.Time               // last UpdatedAt the preview was sourced from
	replies      map[string][]string                // sessionId -> last few assistant text replies (oldest→newest)

	expanded    bool      // whether the selected row is expanded
	expandStart time.Time // wall clock at which the reveal animation began

	// replyReveal tracks per-session typewriter animation state for the
	// most recent assistant reply. We snapshot the previous tail and let
	// the visible char count walk forward over time.
	replyReveal map[string]*replyRevealState

	followupTarget string // tmux target captured when the followup modal opened

	filter   string
	matches  []int // indices into m.sessions for n/N
	matchPos int

	spinner spinner.Model

	modal      modalKind
	input      textinput.Model
	textarea   textarea.Model
	pendingG   bool
	chordGen   int
	firstSeen  map[string]time.Time

	toast       string
	toastClearAt time.Time
}

func NewModel(src source.Source, notifier *notify.Notifier, remote bool, host string) Model {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = Theme.StatusBusy

	ti := textinput.New()
	ti.Prompt = "› "
	ti.CharLimit = 0

	ta := textarea.New()
	ta.Prompt = "│ "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(4)

	return Model{
		src:        src,
		notifier:   notifier,
		remote:     remote,
		host:       host,
		keys:       DefaultKeyMap(),
		spinner:    sp,
		input:      ti,
		textarea:   ta,
		previews:    map[string]string{},
		previewKey:  map[string]time.Time{},
		replies:     map[string][]string{},
		replyReveal: map[string]*replyRevealState{},
		firstSeen:   map[string]time.Time{},
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		loadInitial(m.src),
		fadeTick(),
	)
}

// --- async commands ---

func loadInitial(src source.Source) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ss, err := src.List(ctx)
		return sessionsLoadedMsg{sessions: ss, err: err}
	}
}

func loadTranscript(src source.Source, id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		evs, err := src.Tail(ctx, id, 60)
		return transcriptMsg{id: id, events: evs, err: err}
	}
}

func fadeTick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg { return fadeTickMsg(t) })
}

const expandAnimDuration = 220 * time.Millisecond

func expandTick() tea.Cmd {
	return tea.Tick(20*time.Millisecond, func(t time.Time) tea.Msg { return expandTickMsg(t) })
}

const transcriptPollInterval = 750 * time.Millisecond

func transcriptTick(id string) tea.Cmd {
	return tea.Tick(transcriptPollInterval, func(time.Time) tea.Msg {
		return transcriptTickMsg{id: id}
	})
}

// replyRevealState animates a typewriter effect on the latest assistant
// reply. `target` is the full text we're animating toward; `visible` is how
// many runes of it are currently shown. While visible < len(target runes),
// we keep ticking and incrementing visible.
type replyRevealState struct {
	target  []rune
	visible int
	prefix  int // runes that were already on screen before this round
}

const replyAnimInterval = 30 * time.Millisecond
const replyCharsPerTick = 6

func replyAnimTick(id string) tea.Cmd {
	return tea.Tick(replyAnimInterval, func(time.Time) tea.Msg {
		return replyAnimTickMsg{id: id}
	})
}

func chordTimeout(gen int) tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		return chordTimeoutMsg{generation: gen}
	})
}

func toastClearCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return toastClearMsg{} })
}

// --- update ---

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()

	case sessionsLoadedMsg:
		if msg.err != nil {
			m = m.setToast("error: " + msg.err.Error())
			return m, toastClearCmd(4 * time.Second)
		}
		now := time.Now()
		for _, s := range msg.sessions {
			if _, ok := m.firstSeen[s.ID]; !ok {
				m.firstSeen[s.ID] = now
			}
		}
		m.sessions = msg.sessions
		m.sortSessions()
		if m.selected == "" && len(m.sessions) > 0 {
			m.selected = m.sessions[0].ID
		}
		cmds = append(cmds, m.maybeLoadPreview())

	case sessionUpsertMsg:
		if _, ok := m.firstSeen[msg.session.ID]; !ok {
			m.firstSeen[msg.session.ID] = time.Now()
		}
		prev := m.findSession(msg.session.ID)
		m.upsertSession(msg.session)
		m.sortSessions()
		m.handleStatusTransition(prev, msg.session)
		if msg.session.ID == m.selected {
			cmds = append(cmds, m.maybeLoadPreview())
		}

	case sessionDeleteMsg:
		m.deleteSession(msg.id)
		if m.selected == msg.id {
			if len(m.sessions) > 0 {
				m.selected = m.sessions[0].ID
				cmds = append(cmds, m.maybeLoadPreview())
			} else {
				m.selected = ""
			}
		}

	case transcriptMsg:
		if msg.err == nil {
			if p := claude.Preview(msg.events); p != "" {
				m.previews[msg.id] = p
			}
			newReplies := lastAssistantReplies(msg.events, expandedReplyCount)
			cmd := m.updateReplyReveal(msg.id, newReplies)
			m.replies[msg.id] = newReplies
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			if s := m.findSession(msg.id); s != nil {
				m.previewKey[msg.id] = s.UpdatedAt
			}
		}

	case replyAnimTickMsg:
		st, ok := m.replyReveal[msg.id]
		if ok && st.visible < len(st.target) {
			st.visible += replyCharsPerTick
			if st.visible > len(st.target) {
				st.visible = len(st.target)
			}
			if st.visible < len(st.target) {
				cmds = append(cmds, replyAnimTick(msg.id))
			}
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case fadeTickMsg:
		// Periodic redraw so newly-added rows finish their fade-in. We just
		// rely on a future fadeTick — the View() reads firstSeen each frame.
		cmds = append(cmds, fadeTick())

	case expandTickMsg:
		// Drive frames of the expansion reveal until it's fully open.
		if m.expanded && time.Since(m.expandStart) < expandAnimDuration {
			cmds = append(cmds, expandTick())
		}

	case transcriptTickMsg:
		// Live transcript poll: while the expansion is open on a session,
		// re-read its transcript every transcriptPollInterval. The session
		// file's updatedAt only changes on status flips, so we can't rely
		// on Source.Watch for streaming token output.
		if m.expanded && msg.id == m.selected {
			cmds = append(cmds, loadTranscript(m.src, msg.id), m.transcriptPoll())
		}

	case chordTimeoutMsg:
		if msg.generation == m.chordGen {
			m.pendingG = false
		}

	case toastClearMsg:
		if time.Now().After(m.toastClearAt) {
			m.toast = ""
		}

	case toastMsg:
		m = m.setToast(msg.text)
		cmds = append(cmds, toastClearCmd(3*time.Second))

	case attachExitedMsg:
		if msg.err != nil {
			m = m.setToast("attach error: " + msg.err.Error())
			cmds = append(cmds, toastClearCmd(3*time.Second))
		}
		if m.selected != "" {
			cmds = append(cmds, loadTranscript(m.src, m.selected))
		}

	case tea.KeyMsg:
		var cmd tea.Cmd
		m, cmd = m.handleKey(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.modal != modalNone {
		return m.handleModalKey(msg)
	}

	if key.Matches(msg, m.keys.HardQuit) {
		return m, tea.Quit
	}
	if key.Matches(msg, m.keys.Quit) {
		return m, tea.Quit
	}
	if key.Matches(msg, m.keys.Help) {
		m.modal = modalHelp
		return m, nil
	}
	if key.Matches(msg, m.keys.Cmd) {
		m.modal = modalCmd
		m.input.SetValue("")
		m.input.Prompt = ":"
		m.input.Focus()
		return m, textinput.Blink
	}
	if key.Matches(msg, m.keys.Filter) {
		m.modal = modalFilter
		m.input.SetValue(m.filter)
		m.input.Prompt = "/"
		m.input.Focus()
		return m, textinput.Blink
	}
	if key.Matches(msg, m.keys.Followup) {
		s := m.findSession(m.selected)
		if s == nil {
			return m, nil
		}
		target := m.findContainingPane(s.PID)
		if target == "" {
			return m.setToast("not in tmux; press o to attach"),
				toastClearCmd(3 * time.Second)
		}
		m.followupTarget = target
		m.modal = modalFollowup
		w := m.followupWidth()
		m.textarea.SetWidth(w)
		m.textarea.SetValue("")
		m.textarea.Focus()
		return m, textarea.Blink
	}
	if key.Matches(msg, m.keys.Expand) {
		if m.expanded {
			m.expanded = false
			return m, nil
		}
		m.expanded = true
		m.expandStart = time.Now()
		return m, tea.Batch(expandTick(), m.forceLoadPreview(), m.transcriptPoll())
	}
	if key.Matches(msg, m.keys.Attach) {
		s := m.findSession(m.selected)
		if s == nil {
			return m, nil
		}
		return m, m.attachCmd(*s)
	}
	if key.Matches(msg, m.keys.Refresh) {
		return m, loadInitial(m.src)
	}

	// vim chord: gg
	if msg.String() == "g" {
		if m.pendingG {
			m.pendingG = false
			m.moveTop()
			return m, nil
		}
		m.pendingG = true
		m.chordGen++
		return m, chordTimeout(m.chordGen)
	}
	if m.pendingG {
		m.pendingG = false
	}

	switch {
	case key.Matches(msg, m.keys.Down):
		m.move(1)
		return m, m.afterMoveCmd()
	case key.Matches(msg, m.keys.Up):
		m.move(-1)
		return m, m.afterMoveCmd()
	case key.Matches(msg, m.keys.PageDown):
		m.move(10)
		return m, m.afterMoveCmd()
	case key.Matches(msg, m.keys.PageUp):
		m.move(-10)
		return m, m.afterMoveCmd()
	case key.Matches(msg, m.keys.Bottom):
		m.moveBottom()
		return m, m.afterMoveCmd()
	case key.Matches(msg, m.keys.NextMatch):
		m.cycleMatch(1)
		return m, m.afterMoveCmd()
	case key.Matches(msg, m.keys.PrevMatch):
		m.cycleMatch(-1)
		return m, m.afterMoveCmd()
	}

	return m, nil
}

// afterMoveCmd handles the side effects of changing the selected row: kick
// off a transcript reload, and if the expansion is open, restart its reveal
// animation so it lands on the new row instead of jumping fully drawn.
func (m *Model) afterMoveCmd() tea.Cmd {
	cmds := []tea.Cmd{m.maybeLoadPreview()}
	if m.expanded {
		m.expandStart = time.Now()
		cmds = append(cmds, expandTick(), m.transcriptPoll())
	}
	return tea.Batch(cmds...)
}

// forceLoadPreview triggers a transcript fetch regardless of cache state.
// Used when the user opens the expansion so we always show fresh content.
func (m Model) forceLoadPreview() tea.Cmd {
	if m.selected == "" {
		return nil
	}
	return loadTranscript(m.src, m.selected)
}

// transcriptPoll schedules the next transcript re-read for the selected
// session. The handler decides whether to actually issue the read (only if
// the expansion is still open on the same row).
func (m Model) transcriptPoll() tea.Cmd {
	if m.selected == "" {
		return nil
	}
	return transcriptTick(m.selected)
}

func (m Model) handleModalKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		m.modal = modalNone
		m.input.Blur()
		m.textarea.Blur()
		return m, nil
	}

	switch m.modal {
	case modalHelp:
		m.modal = modalNone
		return m, nil

	case modalFilter:
		if msg.Type == tea.KeyEnter {
			m.filter = strings.TrimSpace(m.input.Value())
			m.refreshMatches()
			m.modal = modalNone
			m.input.Blur()
			if len(m.matches) > 0 {
				m.selected = m.sessions[m.matches[0]].ID
				m.matchPos = 0
				return m, m.maybeLoadPreview()
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd

	case modalCmd:
		if msg.Type == tea.KeyEnter {
			cmd := strings.TrimSpace(m.input.Value())
			m.modal = modalNone
			m.input.Blur()
			return m.runCommand(cmd)
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd

	case modalFollowup:
		// Ctrl+Enter (or Ctrl+J / Ctrl+D) sends; plain Enter inserts a newline.
		if msg.Type == tea.KeyCtrlD ||
			(msg.Type == tea.KeyEnter && msg.Alt) ||
			msg.String() == "ctrl+j" {
			text := m.textarea.Value()
			target := m.followupTarget
			m.modal = modalNone
			m.textarea.Blur()
			if target == "" || strings.TrimSpace(text) == "" {
				return m, nil
			}
			remote, host := m.remote, m.host
			return m, func() tea.Msg {
				if remote {
					if err := remoteSendKeys(host, target, text); err != nil {
						return toastMsg{text: "send-keys: " + err.Error()}
					}
				} else {
					if err := tmuxctl.SendKeys(target, text); err != nil {
						return toastMsg{text: "send-keys: " + err.Error()}
					}
				}
				return toastMsg{text: "sent → " + target}
			}
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd

	}

	return m, nil
}

func (m Model) runCommand(cmd string) (Model, tea.Cmd) {
	switch {
	case cmd == "q" || cmd == "quit":
		return m, tea.Quit
	case strings.HasPrefix(cmd, "filter "):
		m.filter = strings.TrimSpace(strings.TrimPrefix(cmd, "filter "))
		m.refreshMatches()
		return m, nil
	case cmd == "":
		return m, nil
	default:
		return m.setToast("unknown command: " + cmd), toastClearCmd(2 * time.Second)
	}
}

// attachCmd takes over the terminal so the user can interact with a running
// claude session. Strategy:
//
//  1. If the running claude lives inside a tmux pane, attach to that tmux
//     pane directly. This is a true attach — same process, same state, every
//     keystroke is live in any other terminal also viewing the pane.
//  2. Otherwise fall back to `claude --resume`, which spawns a new claude
//     against the same on-disk transcript. State is duplicated and not live;
//     this is what we used to do unconditionally.
func (m Model) attachCmd(s claude.Session) tea.Cmd {
	if target := m.findContainingPane(s.PID); target != "" {
		return m.tmuxAttachCmd(target)
	}
	var c *exec.Cmd
	if m.remote {
		// `bash -lc` sources the remote login profile so PATH picks up tools
		// installed under ~/.local/bin, asdf shims, nvm, etc. Without this,
		// non-interactive ssh sees a minimal PATH and reports "command not found".
		script := fmt.Sprintf("cd %s && exec claude --resume %s",
			shellQuote(s.CWD), shellQuote(s.ID))
		c = exec.Command("ssh", "-t", m.host, "--", "bash", "-lc", script)
	} else {
		c = exec.Command("claude", "--resume", s.ID)
		c.Dir = s.CWD
	}
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return attachExitedMsg{err: err}
	})
}

func (m Model) findContainingPane(pid int) string {
	if pid <= 0 {
		return ""
	}
	if m.remote {
		full := "lookup() {" + tmuxctl.RemoteTmuxLookupScript + "}; lookup " + strconv.Itoa(pid)
		out, err := exec.Command("ssh", m.host, "--", "bash", "-lc", full).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	target, err := tmuxctl.FindPaneByPID(pid)
	if err != nil {
		return ""
	}
	return target
}

// tmuxAttachCmd attaches the user's terminal to an existing tmux pane.
// Detach with the standard tmux prefix-d (Ctrl+B D by default) — this returns
// to the whip TUI without killing the underlying claude.
func (m Model) tmuxAttachCmd(target string) tea.Cmd {
	// target is "session:window.pane"; tmux attach wants the session name.
	session := target
	if i := strings.IndexAny(target, ":"); i > 0 {
		session = target[:i]
	}

	var c *exec.Cmd
	if m.remote {
		// Switch the session's active pane to the one we want, then attach.
		script := fmt.Sprintf(
			"tmux select-pane -t %s 2>/dev/null; exec tmux attach-session -t %s",
			shellQuote(target), shellQuote(session))
		c = exec.Command("ssh", "-t", m.host, "--", "bash", "-lc", script)
	} else {
		// Same idea locally: nudge the active pane, then attach.
		_ = exec.Command("tmux", "select-pane", "-t", target).Run()
		c = exec.Command("tmux", "attach-session", "-t", session)
	}
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return attachExitedMsg{err: err}
	})
}

// shellQuote wraps s in single quotes for safe inclusion in a sh -c command.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func remoteSendKeys(host, target, text string) error {
	script := fmt.Sprintf("tmux send-keys -t %s -l %s && tmux send-keys -t %s Enter",
		shellQuote(target), shellQuote(text), shellQuote(target))
	c := exec.Command("ssh", host, "--", "bash", "-lc", script)
	return c.Run()
}

// --- session list management ---

func (m *Model) findSession(id string) *claude.Session {
	for i := range m.sessions {
		if m.sessions[i].ID == id {
			return &m.sessions[i]
		}
	}
	return nil
}

func (m *Model) upsertSession(s claude.Session) {
	for i := range m.sessions {
		if m.sessions[i].ID == s.ID {
			// preserve fields the file does not carry
			s.Origin = m.sessions[i].Origin
			s.TmuxTarget = m.sessions[i].TmuxTarget
			s.Preview = m.sessions[i].Preview
			m.sessions[i] = s
			return
		}
	}
	m.sessions = append(m.sessions, s)
}

func (m *Model) deleteSession(id string) {
	for i := range m.sessions {
		if m.sessions[i].ID == id {
			m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
			return
		}
	}
}

func (m *Model) sortSessions() {
	sort.Slice(m.sessions, func(i, j int) bool {
		// Busy first, then needs-input, idle, stopped — within tier, recent first.
		ri, rj := tier(m.sessions[i].Status), tier(m.sessions[j].Status)
		if ri != rj {
			return ri < rj
		}
		return m.sessions[i].UpdatedAt.After(m.sessions[j].UpdatedAt)
	})
}

func tier(s claude.Status) int {
	switch s {
	case claude.StatusNeedsInput:
		return 0
	case claude.StatusBusy:
		return 1
	case claude.StatusIdle:
		return 2
	default:
		return 3
	}
}

func (m *Model) selectedIndex() int {
	for i := range m.sessions {
		if m.sessions[i].ID == m.selected {
			return i
		}
	}
	return -1
}

func (m *Model) move(delta int) {
	i := m.selectedIndex()
	if i < 0 {
		if len(m.sessions) > 0 {
			m.selected = m.sessions[0].ID
		}
		return
	}
	i += delta
	if i < 0 {
		i = 0
	}
	if i >= len(m.sessions) {
		i = len(m.sessions) - 1
	}
	m.selected = m.sessions[i].ID
}

func (m *Model) moveTop() {
	if len(m.sessions) > 0 {
		m.selected = m.sessions[0].ID
	}
}

func (m *Model) moveBottom() {
	if n := len(m.sessions); n > 0 {
		m.selected = m.sessions[n-1].ID
	}
}

func (m *Model) refreshMatches() {
	m.matches = m.matches[:0]
	if m.filter == "" {
		return
	}
	needle := strings.ToLower(m.filter)
	for i, s := range m.sessions {
		hay := strings.ToLower(s.CWD + " " + s.Name + " " + s.Status.String())
		if strings.Contains(hay, needle) {
			m.matches = append(m.matches, i)
		}
	}
}

func (m *Model) cycleMatch(delta int) {
	if len(m.matches) == 0 {
		return
	}
	m.matchPos = (m.matchPos + delta + len(m.matches)) % len(m.matches)
	m.selected = m.sessions[m.matches[m.matchPos]].ID
}

func (m Model) maybeLoadPreview() tea.Cmd {
	s := m.findSession(m.selected)
	if s == nil {
		return nil
	}
	if last, ok := m.previewKey[s.ID]; ok && last.Equal(s.UpdatedAt) {
		return nil
	}
	return loadTranscript(m.src, s.ID)
}

func (m Model) handleStatusTransition(prev *claude.Session, next claude.Session) {
	if m.notifier == nil {
		return
	}
	if prev == nil {
		return
	}
	cwd := filepath.Base(next.CWD)
	if prev.Status == claude.StatusBusy && next.Status == claude.StatusIdle {
		m.notifier.Send(next.ID+":idle",
			"Claude finished",
			fmt.Sprintf("%s — %s", cwd, m.host))
	}
	if prev.Status != claude.StatusNeedsInput && next.Status == claude.StatusNeedsInput {
		m.notifier.Send(next.ID+":need",
			"Claude needs input",
			fmt.Sprintf("%s — %s", cwd, m.host))
	}
	if prev.Status != claude.StatusStopped && next.Status == claude.StatusStopped {
		m.notifier.Send(next.ID+":stop",
			"Claude session stopped",
			fmt.Sprintf("%s — %s", cwd, m.host))
	}
}

func (m Model) setToast(text string) Model {
	m.toast = text
	m.toastClearAt = time.Now().Add(3 * time.Second)
	return m
}

// --- layout ---

func (m *Model) layout() {
	// Content height = total - header - footer - 2 border lines.
	if m.height < 8 {
		return
	}
}

// followupWidth caps the textarea width so long prompts wrap instead of
// expanding the modal indefinitely.
func (m Model) followupWidth() int {
	w := m.width - 12 // modal border + padding + buffer
	if w > 80 {
		w = 80
	}
	if w < 30 {
		w = 30
	}
	return w
}

// listInnerWidth is the width available for a row's text content inside the
// bordered list box.
func (m Model) listInnerWidth() int {
	w := m.width - 4 // 2 border + 2 padding
	if w < 20 {
		w = 20
	}
	return w
}

func (m Model) listInnerHeight() int {
	h := m.height - 4 // header + footer + 2 borders
	if h < 4 {
		h = 4
	}
	return h
}

// --- view ---

func (m Model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	header := m.renderHeader()
	footer := m.renderFooter()

	innerW := m.listInnerWidth()
	innerH := m.listInnerHeight()

	body := Theme.BorderActive.
		Width(innerW + 2).
		Height(innerH).
		Padding(0, 1).
		Render(m.renderList(innerW, innerH))

	v := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)

	if m.modal != modalNone {
		v = m.overlayModal(v)
	}
	if m.toast != "" {
		v = m.overlayToast(v)
	}
	return v
}

func (m Model) renderHeader() string {
	host := m.host
	if !m.remote {
		host = "local"
	}
	parts := []string{
		Theme.Title.Render("whip"),
		Theme.HeaderDim.Render("·"),
		Theme.Header.Render(host),
		Theme.HeaderDim.Render(fmt.Sprintf("· %d sessions", len(m.sessions))),
	}
	busy := 0
	for _, s := range m.sessions {
		if s.Status == claude.StatusBusy {
			busy++
		}
	}
	if busy > 0 {
		parts = append(parts,
			Theme.HeaderDim.Render("·"),
			Theme.StatusBusy.Render(fmt.Sprintf("%s %d busy", m.spinner.View(), busy)),
		)
	}
	return lipgloss.NewStyle().Padding(0, 1).Render(strings.Join(parts, " "))
}

func (m Model) renderFooter() string {
	hint := "j/k move  e expand  o attach  f follow-up  / filter  : cmd  ? help  q quit"
	return Theme.Footer.Padding(0, 1).Render(hint)
}

// renderList groups sessions by status under colored section headers, then
// fits the result to the available height. Each session is one line:
// "<dot> <cwd>     <preview…>      <age>".
func (m Model) renderList(w, h int) string {
	if len(m.sessions) == 0 {
		return Theme.Hint.Render("no sessions yet — start `claude` somewhere and it'll show up")
	}

	// Group sessions by status. m.sessions is already sorted by tier+recency,
	// so a single pass keeps tier order stable.
	groups := []struct {
		status claude.Status
		title  string
		items  []claude.Session
	}{
		{claude.StatusNeedsInput, "Needs input", nil},
		{claude.StatusBusy, "Working", nil},
		{claude.StatusIdle, "Idle", nil},
		{claude.StatusStopped, "Stopped", nil},
	}
	for _, s := range m.sessions {
		for i := range groups {
			if groups[i].status == s.Status {
				groups[i].items = append(groups[i].items, s)
				break
			}
		}
	}

	var lines []string
	first := true
	for _, g := range groups {
		if len(g.items) == 0 {
			continue
		}
		if !first {
			lines = append(lines, "")
		}
		first = false
		_, gs := StatusGlyph(g.status.String())
		header := gs.Bold(true).Render(g.title) +
			Theme.Hint.Render(fmt.Sprintf("  %d", len(g.items)))
		lines = append(lines, header)
		for _, s := range g.items {
			lines = append(lines, m.renderRow(s, w))
			if s.ID == m.selected && m.expanded {
				lines = append(lines, m.renderRepliesExpansion(s, w)...)
			}
		}
	}

	// Crop to available height.
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

// renderRow is one session line, padded to width w so the selection
// background paints the full row.
func (m Model) renderRow(s claude.Session, w int) string {
	statusStr := s.Status.String()
	glyph, gStyle := StatusGlyph(statusStr)
	dot := gStyle.Render(glyph)
	if s.Status == claude.StatusBusy {
		dot = gStyle.Render(m.spinner.View())
	}

	cwd := filepath.Base(s.CWD)
	if cwd == "" {
		cwd = s.CWD
	}
	age := relTime(s.UpdatedAt)

	preview := s.Preview
	if p, ok := m.previews[s.ID]; ok && p != "" {
		preview = p
	}

	// Layout: "  <dot> <cwd>   <preview>          <age>"
	left := "  " + dot + " " + cwd
	leftW := lipgloss.Width(left)

	// Reserve right side for age (padded to a fixed slot for alignment).
	const ageSlot = 5
	ageStr := age
	if lipgloss.Width(ageStr) > ageSlot {
		ageStr = ageStr[:ageSlot]
	}
	rightPad := strings.Repeat(" ", ageSlot-lipgloss.Width(ageStr))
	rightRendered := rightPad + Theme.Hint.Render(ageStr)
	rightW := ageSlot

	mid := w - leftW - rightW - 2
	if mid < 0 {
		mid = 0
	}
	previewRendered := ""
	if mid > 4 && preview != "" {
		previewRendered = "  " + Theme.Hint.Render(truncate(preview, mid-2))
	}
	pad := w - leftW - lipgloss.Width(previewRendered) - rightW
	if pad < 1 {
		pad = 1
	}
	line := left + previewRendered + strings.Repeat(" ", pad) + rightRendered

	if s.ID == m.selected {
		return Theme.RowSelected.Width(w).Render(line)
	}
	if first, ok := m.firstSeen[s.ID]; ok {
		if time.Since(first) < 400*time.Millisecond {
			return lipgloss.NewStyle().
				Foreground(lipgloss.Color("242")).
				Width(w).
				Render(line)
		}
	}
	return lipgloss.NewStyle().Width(w).Render(line)
}

const expandedReplyCount = 3

// renderRepliesExpansion is the inline expansion shown under the selected
// row when m.expanded is true. It animates open by progressively revealing
// lines based on how long ago the user pressed `e`, and if the agent is
// currently busy it shows a "thinking" indicator at the bottom.
func (m Model) renderRepliesExpansion(s claude.Session, w int) []string {
	const indent = "      "
	innerW := w - len(indent)
	if innerW < 10 {
		innerW = 10
	}

	var lines []string

	replies, loaded := m.replies[s.ID]
	switch {
	case !loaded:
		lines = []string{indent + Theme.Hint.Render("loading…")}
	case len(replies) == 0:
		lines = []string{indent + Theme.Hint.Render("no replies yet")}
	default:
		// Apply typewriter reveal to the latest reply only.
		display := make([]string, len(replies))
		copy(display, replies)
		if st, ok := m.replyReveal[s.ID]; ok && st.visible < len(st.target) {
			display[len(display)-1] = string(st.target[:st.visible])
		}
		for i, r := range display {
			marker := Theme.StatusIdle.Render("▸ ")
			first := true
			for _, ln := range wrapPlain(r, innerW-2) {
				prefix := indent + "  "
				if first {
					prefix = indent + marker
					first = false
				}
				lines = append(lines, prefix+Theme.RowDim.Render(ln))
			}
			// Trailing cursor on the latest reply while it's still revealing.
			if i == len(display)-1 {
				if st, ok := m.replyReveal[s.ID]; ok && st.visible < len(st.target) {
					if n := len(lines); n > 0 {
						lines[n-1] += Theme.StatusBusy.Render("▍")
					}
				}
			}
		}
	}

	if s.Status == claude.StatusBusy {
		lines = append(lines,
			indent+Theme.StatusBusy.Render(m.spinner.View()+" thinking…"))
	}

	// Reveal animation: progressive line count over expandAnimDuration.
	elapsed := time.Since(m.expandStart)
	if elapsed < expandAnimDuration && len(lines) > 0 {
		ratio := float64(elapsed) / float64(expandAnimDuration)
		visible := int(ratio*float64(len(lines))) + 1
		if visible > len(lines) {
			visible = len(lines)
		}
		lines = lines[:visible]
	}

	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = lipgloss.NewStyle().Width(w).Render(ln)
	}
	return out
}

// updateReplyReveal compares old and new replies for a session and, if the
// latest reply has grown (new tokens streamed in), schedules a typewriter
// animation that reveals the new tail. Returns the animation command if one
// should start, or nil. The reveal is only enabled while the user is looking
// at the expansion for that session — otherwise we snap to fully revealed.
func (m *Model) updateReplyReveal(id string, fresh []string) tea.Cmd {
	if len(fresh) == 0 {
		return nil
	}
	latest := fresh[len(fresh)-1]
	if !m.expanded || id != m.selected {
		// Not visible — keep animation state in sync but don't kick a tick.
		m.replyReveal[id] = &replyRevealState{
			target:  []rune(latest),
			visible: len([]rune(latest)),
			prefix:  len([]rune(latest)),
		}
		return nil
	}

	prev, hadPrev := m.replyReveal[id]
	prevReplies := m.replies[id]
	prevLatest := ""
	if len(prevReplies) > 0 {
		prevLatest = prevReplies[len(prevReplies)-1]
	}

	target := []rune(latest)

	// New reply altogether (different message, or first time we see one):
	// animate from scratch.
	if !hadPrev || !strings.HasPrefix(latest, prevLatest) ||
		len(prevReplies) != len(fresh) {
		m.replyReveal[id] = &replyRevealState{target: target, visible: 0, prefix: 0}
		return replyAnimTick(id)
	}

	// Same reply growing: keep what's been shown, animate the new tail.
	prev.target = target
	if prev.visible > len(target) {
		prev.visible = len(target)
	}
	if prev.visible < len(target) {
		return replyAnimTick(id)
	}
	return nil
}

// lastAssistantReplies pulls the trailing N assistant text replies out of a
// transcript. Tool use/results and any non-text content are excluded so we
// only show what Claude actually said back to the user.
func lastAssistantReplies(events []claude.TranscriptEvent, n int) []string {
	if n <= 0 {
		return nil
	}
	var out []string
	for i := len(events) - 1; i >= 0 && len(out) < n; i-- {
		e := events[i]
		if e.Type != "assistant" {
			continue
		}
		text := strings.TrimSpace(e.Text)
		if text == "" {
			continue
		}
		out = append([]string{text}, out...)
	}
	return out
}

// wrapPlain hard-wraps s at width w, breaking on spaces where possible. Used
// for the dim reply text under the selected row — needs no ANSI awareness.
func wrapPlain(s string, w int) []string {
	if w <= 0 {
		return []string{s}
	}
	s = strings.ReplaceAll(s, "\r", "")
	var out []string
	for _, para := range strings.Split(s, "\n") {
		para = strings.TrimRight(para, " \t")
		if para == "" {
			out = append(out, "")
			continue
		}
		for len(para) > w {
			cut := w
			if i := strings.LastIndex(para[:w], " "); i > w/2 {
				cut = i
			}
			out = append(out, strings.TrimRight(para[:cut], " "))
			para = strings.TrimLeft(para[cut:], " ")
		}
		if para != "" {
			out = append(out, para)
		}
	}
	const maxLines = 4
	if len(out) > maxLines {
		out = out[:maxLines]
		out[maxLines-1] = truncate(out[maxLines-1], w-1) + "…"
	}
	return out
}

func (m Model) overlayModal(base string) string {
	var body string
	switch m.modal {
	case modalHelp:
		body = m.renderHelp()
	case modalFilter, modalCmd:
		body = m.input.View()
		switch m.modal {
		case modalFilter:
			body = "Filter sessions\n\n" + body + "\n\nEnter to apply  Esc to cancel"
		case modalCmd:
			body = "Command\n\n" + body + "\n\n:q  :filter <text>"
		}
	case modalFollowup:
		body = "Send follow-up → " + m.followupTarget + "\n\n" +
			m.textarea.View() + "\n\n" +
			Theme.Hint.Render("Enter newline  Ctrl+D send  Esc cancel")
	}
	box := Theme.Modal.Render(body)
	bw := lipgloss.Width(box)
	bh := lipgloss.Height(box)
	x := max(0, (m.width-bw)/2)
	y := max(0, (m.height-bh)/2)
	return placeOver(base, box, x, y, m.width, m.height)
}

func (m Model) renderHelp() string {
	rows := [][2]string{
		{"j / k", "move down / up"},
		{"gg / G", "top / bottom"},
		{"^u / ^d", "page up / down"},
		{"/", "filter"},
		{"n / N", "next / prev match"},
		{"o, ↵", "attach (claude --resume)"},
		{"e", "expand recent replies"},
		{"f", "send follow-up (whip-managed)"},
		{"r", "refresh now"},
		{":", "command (e.g. :q)"},
		{"q, ^c", "quit"},
		{"esc", "cancel modal"},
	}
	var b strings.Builder
	b.WriteString(Theme.Title.Render("whip — help") + "\n\n")
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("  %-10s  %s\n", Theme.Header.Render(r[0]), r[1]))
	}
	b.WriteString("\nany key closes")
	return b.String()
}

func (m Model) overlayToast(base string) string {
	box := Theme.Toast.Render(m.toast)
	x := max(0, m.width-lipgloss.Width(box)-2)
	y := max(0, m.height-3)
	return placeOver(base, box, x, y, m.width, m.height)
}

// placeOver draws "over" on top of "base" at (x, y). lipgloss does not have a
// proper composite, so we line-by-line splice. Both are rectangular blocks.
func placeOver(base, over string, x, y, w, h int) string {
	if base == "" {
		return over
	}
	baseLines := strings.Split(base, "\n")
	overLines := strings.Split(over, "\n")
	for i, ol := range overLines {
		row := y + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		baseLines[row] = spliceAt(baseLines[row], ol, x, w)
	}
	return strings.Join(baseLines, "\n")
}

// spliceAt overwrites a substring at column x in line. Width-aware via lipgloss.
// Falls back to byte splicing for ASCII (good enough — our overlays are ASCII).
func spliceAt(line, over string, x, totalW int) string {
	lineW := lipgloss.Width(line)
	if lineW < x {
		line = line + strings.Repeat(" ", x-lineW)
	}
	overW := lipgloss.Width(over)
	prefix := truncateANSI(line, x)
	suffixStart := x + overW
	suffix := ""
	if suffixStart < lipgloss.Width(line) {
		// Naive: drop suffix to avoid messing up ANSI mid-stream. Modal/toast
		// already cover what's underneath.
		_ = suffix
	}
	out := prefix + over + suffix
	if lipgloss.Width(out) > totalW {
		out = truncateANSI(out, totalW)
	}
	return out
}

func truncateANSI(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	// Crude truncate — strip ANSI safety by re-rendering plain. Modals/toasts
	// don't need byte-perfect underneath.
	return lipgloss.NewStyle().MaxWidth(n).Render(s)
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func relTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Second:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
