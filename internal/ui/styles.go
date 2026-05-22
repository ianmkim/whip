package ui

import "github.com/charmbracelet/lipgloss"

// Theme is a small palette tuned for dark terminals. We avoid full-screen
// background fills so terminal transparency keeps working.
var Theme = struct {
	Header      lipgloss.Style
	HeaderDim   lipgloss.Style
	Footer      lipgloss.Style
	StatusIdle  lipgloss.Style
	StatusBusy  lipgloss.Style
	StatusNeed  lipgloss.Style
	StatusStop  lipgloss.Style
	RowSelected lipgloss.Style
	RowDim      lipgloss.Style
	Title       lipgloss.Style
	Hint        lipgloss.Style
	Toast       lipgloss.Style
	Modal       lipgloss.Style
}{
	Header:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")),
	HeaderDim:   lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
	Footer:      lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
	StatusIdle:  lipgloss.NewStyle().Foreground(lipgloss.Color("78")),
	StatusBusy:  lipgloss.NewStyle().Foreground(lipgloss.Color("220")),
	StatusNeed:  lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
	StatusStop:  lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
	RowSelected: lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Background(lipgloss.Color("237")).Bold(true),
	RowDim:      lipgloss.NewStyle().Foreground(lipgloss.Color("250")),
	Title:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("117")),
	Hint:        lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
	Toast:       lipgloss.NewStyle().Padding(0, 1).Background(lipgloss.Color("236")).Foreground(lipgloss.Color("231")),
	Modal:       lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("99")).Padding(1, 2),
}

// Icon is the whip mascot rendered above the header.
const Icon = "╞▤▤▤╡〰╮\n       ╰୭"

func StatusGlyph(s string) (string, lipgloss.Style) {
	switch s {
	case "idle":
		return "●", Theme.StatusIdle
	case "busy":
		return "◐", Theme.StatusBusy
	case "needs-input":
		return "◑", Theme.StatusNeed
	case "stopped":
		return "○", Theme.StatusStop
	default:
		return "·", Theme.StatusStop
	}
}
