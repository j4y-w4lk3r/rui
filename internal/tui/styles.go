package tui

import "github.com/charmbracelet/lipgloss"

// Catppuccin Mocha palette, same family as ttcli.
// Pad styles leave the terminal background alone so the theme shows through.
var (
	colorBase    = lipgloss.Color("#1e1e2e")
	colorOverlay = lipgloss.Color("#45475a")
	colorText    = lipgloss.Color("#cdd6f4")
	colorSubtext = lipgloss.Color("#a6adc8")
	colorMuted   = lipgloss.Color("#6c7086")
	colorMauve   = lipgloss.Color("#cba6f7")
	colorBlue    = lipgloss.Color("#89b4fa")
	colorGreen   = lipgloss.Color("#a6e3a1")
	colorYellow  = lipgloss.Color("#f9e2af")
	colorPeach   = lipgloss.Color("#fab387")
	colorRed     = lipgloss.Color("#f38ba8")
	colorTeal    = lipgloss.Color("#94e2d5")

	// Names still used by older call sites.
	colorOrange = colorPeach
	colorDim    = colorMuted
	colorOK     = colorGreen
	colorBad    = colorRed
)

var (
	rowPadStyle = lipgloss.NewStyle()

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorText)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBlue)

	headerBrandStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorText)

	headerSubStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	locationStyle = lipgloss.NewStyle().
			Foreground(colorPeach).
			Bold(true)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorSubtext)

	paneBlurred = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorOverlay).
			Padding(0, 1)

	paneFocused = paneBlurred.BorderForeground(colorMauve)

	menuItemStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Padding(0, 1)

	menuItemSel = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(colorBase).
			Background(colorMauve).
			Bold(true)

	keyStyle = lipgloss.NewStyle().Foreground(colorMuted).Width(16)
	valStyle = lipgloss.NewStyle().Foreground(colorText)

	okStyle   = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	badStyle  = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	hintStyle = lipgloss.NewStyle().Foreground(colorMuted)

	tileBase = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorOverlay).
			Padding(0, 1).
			Width(18).
			Height(4)
	tileSel = tileBase.
		BorderForeground(colorMauve).
		Bold(true)

	tileTitleStyle = lipgloss.NewStyle().Foreground(colorText).Bold(true)
	tileOkStyle    = lipgloss.NewStyle().Foreground(colorGreen)
	tileBadStyle   = lipgloss.NewStyle().Foreground(colorRed)
	tileDimStyle   = lipgloss.NewStyle().Foreground(colorMuted)

	helpBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMauve).
			Padding(1, 2)

	helpMarkerStyle = rowPadStyle

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorPeach).
			Bold(true)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(colorSubtext)

	helpTitleInBoxStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorBlue)

	helpHintInBoxStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	helpSearchStyle = lipgloss.NewStyle().
			Foreground(colorTeal).
			Bold(true)

	helpMatchStyle = lipgloss.NewStyle().
			Foreground(colorYellow).
			Bold(true)

	helpSelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBase).
			Background(colorMauve)
)
