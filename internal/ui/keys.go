package ui

import "github.com/charmbracelet/bubbles/key"

// KeyMap is intentionally close to vim defaults. Modal commands (`:`) are
// handled separately in the model.
type KeyMap struct {
	Up         key.Binding
	Down       key.Binding
	Left       key.Binding
	Right      key.Binding
	Top        key.Binding // gg (chord)
	Bottom     key.Binding // G
	PageUp     key.Binding
	PageDown   key.Binding
	Filter     key.Binding // /
	NextMatch  key.Binding // n
	PrevMatch  key.Binding // N
	Attach     key.Binding // o or Enter
	Followup   key.Binding // f
	Expand     key.Binding // e — toggle reply expansion under selected row
	Refresh    key.Binding // r
	Help       key.Binding // ?
	Cmd        key.Binding // : modal command
	Quit       key.Binding // q (only outside modal)
	HardQuit   key.Binding // ctrl+c
	Cancel     key.Binding // esc
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:        key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
		Down:      key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
		Left:      key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/←", "list")),
		Right:     key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l/→", "detail")),
		Top:       key.NewBinding(key.WithKeys("g"), key.WithHelp("gg", "top")),
		Bottom:    key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "bottom")),
		PageUp:    key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("^u", "page up")),
		PageDown:  key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("^d", "page down")),
		Filter:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		NextMatch: key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next")),
		PrevMatch: key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "prev")),
		Attach:    key.NewBinding(key.WithKeys("o", "enter"), key.WithHelp("o/↵", "attach")),
		Followup:  key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "follow-up")),
		Expand:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "expand replies")),
		Refresh:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Cmd:       key.NewBinding(key.WithKeys(":"), key.WithHelp(":", "command")),
		Quit:      key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		HardQuit:  key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("^c", "quit")),
		Cancel:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}
