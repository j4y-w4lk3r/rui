package tui

import "github.com/charmbracelet/lipgloss"

// Orange brand-ish palette.
var (
	colorOrange = lipgloss.Color("#FF7900")
	colorDim    = lipgloss.Color("240")
	colorOK     = lipgloss.Color("42")
	colorBad    = lipgloss.Color("196")
	colorBg     = lipgloss.Color("236")
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("0")).
			Background(colorOrange).
			Padding(0, 1)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("250")).
			Background(colorBg).
			Padding(0, 1)

	// paneFocused: the pane the user's keys currently affect — orange border.
	paneFocused = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorOrange).
			Padding(0, 1)

	// paneBlurred: the other pane — dim grey border so the focused side is
	// visually unambiguous.
	paneBlurred = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDim).
			Padding(0, 1)

	menuItemStyle = lipgloss.NewStyle().Padding(0, 1)
	menuItemSel   = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color("0")).
			Background(colorOrange).
			Bold(true)

	keyStyle = lipgloss.NewStyle().Foreground(colorDim).Width(18)
	valStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	okStyle   = lipgloss.NewStyle().Foreground(colorOK).Bold(true)
	badStyle  = lipgloss.NewStyle().Foreground(colorBad).Bold(true)
	hintStyle = lipgloss.NewStyle().Foreground(colorDim).Italic(true)

	// Funbox-style "tile" — a small rectangle showing a label, an icon and
	// a colored status line.
	tileBase = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDim).
			Padding(0, 1).
			Width(18).
			Height(4)
	tileSel = tileBase.Copy().
		BorderForeground(colorOrange).
		Bold(true)

	tileTitleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true)
	tileOkStyle    = lipgloss.NewStyle().Foreground(colorOK)
	tileBadStyle   = lipgloss.NewStyle().Foreground(colorBad)
	tileDimStyle   = lipgloss.NewStyle().Foreground(colorDim)
)
