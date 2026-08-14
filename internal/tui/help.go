package tui

import (
	"fmt"
	"strings"
)

type helpSection struct {
	title string
	lines []string
}

func helpSections() []helpSection {
	return []helpSection{
		{
			title: "Global",
			lines: []string{
				"?          Toggle this help screen",
				"r          Refresh the current tab",
				"R          Refresh all tabs (full reload)",
				"q          Quit",
				"Ctrl+C     Quit",
			},
		},
		{
			title: "Sidebar (left pane)",
			lines: []string{
				"j / k      Move tab selection up / down",
				"↑ / ↓      Same as j / k",
				"l          Enter the highlighted tab",
				"enter      Enter the highlighted tab",
				"Tabs: Overview, Usługi, Devices, Eko, Zaawansowane, Wsparcie, Sesja",
			},
		},
		{
			title: "Tab content (right pane)",
			lines: []string{
				"esc        Return focus to the sidebar",
				"h / j / k / l     Navigate tile grid (vim-style)",
				"← ↑ ↓ →    Same as h / j / k / l on tile grids",
				"enter      Activate the selected tile",
				"t          Toggle Wi-Fi (when data loaded)",
				"g          Toggle guest Wi-Fi",
				"b          Reboot router (confirmation prompt)",
				"x          Log out and quit (Sesja tab only)",
			},
		},
		{
			title: "Devices list",
			lines: []string{
				"j / k      Move selection up / down",
				"↑ / ↓      Same as j / k",
				"home       Jump to first device",
				"end / G    Jump to last device",
				"enter / l  Open device detail",
				"y          Copy selected device IP to clipboard",
				"Y          Copy selected device MAC to clipboard",
				"esc        Back to sidebar",
			},
		},
		{
			title: "Device detail",
			lines: []string{
				"e          Rename device (text input modal)",
				"T          Change device type (picker modal)",
				"y          Copy IP to clipboard",
				"Y          Copy MAC to clipboard",
				"esc / h    Back to device list",
			},
		},
		{
			title: "Rename modal",
			lines: []string{
				"Type the new name, then Enter to save",
				"esc        Cancel and return to device detail",
			},
		},
		{
			title: "Type picker",
			lines: []string{
				"j / k      Move up / down the type list",
				"home       First type",
				"end / G    Last type",
				"enter      Apply selected type",
				"esc        Cancel",
			},
		},
		{
			title: "Language picker",
			lines: []string{
				"j / k      Move up / down",
				"home       First language",
				"end / G    Last language",
				"enter / l  Apply selected language",
				"esc / h    Cancel",
			},
		},
		{
			title: "Confirm dialogs (reboot / logout)",
			lines: []string{
				"y          Confirm",
				"n          Cancel",
				"esc        Cancel",
			},
		},
		{
			title: "Focus",
			lines: []string{
				"◆ orange border = pane that receives keys",
				"Sidebar ◆ → j/k pick tab, l enters tab",
				"Tab ◆ → navigate content; esc returns to sidebar",
			},
		},
	}
}

// helpLines builds the full help document, one terminal row per entry.
func (m Model) helpLines() []string {
	var lines []string
	lines = append(lines, titleStyle.Render(" Help ")+" "+hintStyle.Render("keyboard shortcuts"))
	lines = append(lines, "")

	for _, sec := range helpSections() {
		lines = append(lines, "  "+okStyle.Render(sec.title))
		for _, line := range sec.lines {
			lines = append(lines, "  "+menuItemStyle.Render(line))
		}
		lines = append(lines, "")
	}

	hostLine := fmt.Sprintf("Router: %s", m.host)
	if m.loggedIn && m.client != nil {
		hostLine += fmt.Sprintf("  •  logged in as %s", m.client.Username())
	}
	lines = append(lines, "  "+valStyle.Render(hostLine))
	return lines
}

// helpViewportHeight is how many help body lines fit above the sticky footer.
func (m Model) helpViewportHeight() int {
	h := m.height - 1 // sticky scroll / close bar
	if h < 6 {
		h = 6
	}
	return h
}

func (m Model) helpMaxScroll() int {
	total := len(m.helpLines())
	vis := m.helpViewportHeight()
	if total <= vis {
		return 0
	}
	return total - vis
}

func (m *Model) clampHelpScroll() {
	if m.helpScroll < 0 {
		m.helpScroll = 0
	}
	if max := m.helpMaxScroll(); m.helpScroll > max {
		m.helpScroll = max
	}
}

func (m Model) renderHelpView() string {
	lines := m.helpLines()
	vis := m.helpViewportHeight()
	start := m.helpScroll
	if start > len(lines) {
		start = 0
	}
	end := start + vis
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		b.WriteString(lines[i])
		b.WriteByte('\n')
	}

	maxScroll := m.helpMaxScroll()
	if maxScroll > 0 {
		b.WriteString(hintStyle.Render(fmt.Sprintf(
			"j/k ↑↓ scroll  •  home/end jump  •  lines %d–%d of %d  •  ? or ESC close",
			start+1, end, len(lines),
		)))
	} else {
		b.WriteString(hintStyle.Render("Press ? or ESC to close"))
	}
	return b.String()
}

func (m Model) handleHelpKeys(key string) Model {
	switch key {
	case "?", "esc":
		m.showHelp = false
		m.helpScroll = 0
	case "down", "j":
		m.helpScroll++
		m.clampHelpScroll()
	case "up", "k":
		m.helpScroll--
		m.clampHelpScroll()
	case "home":
		m.helpScroll = 0
	case "end", "G":
		m.helpScroll = m.helpMaxScroll()
	case "pgdown", "ctrl+d":
		m.helpScroll += m.helpViewportHeight() - 1
		m.clampHelpScroll()
	case "pgup", "ctrl+u":
		m.helpScroll -= m.helpViewportHeight() - 1
		m.clampHelpScroll()
	}
	return m
}
