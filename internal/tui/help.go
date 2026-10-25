package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type helpRow struct {
	key     string
	desc    string
	section bool
}

func helpSection(title string) helpRow {
	return helpRow{desc: title, section: true}
}

var helpRows = []helpRow{
	helpSection("Locations"),
	{"p", "choose Warsaw or Wrocław without leaving the TUI", false},
	{"-profile", "PLH3 Wrocław · PLH1 Warsaw · auto via WiFi SSID", false},
	{"", "On Ethernet use -profile PLH3 or -location PLH3", false},
	{"", "Plain rui may fall back to stale ~/.config/rui/.env", false},

	helpSection("Global"),
	{"?", "toggle this help menu", false},
	{"r", "refresh the current tab", false},
	{"R", "refresh all tabs", false},
	{"q / Ctrl+C", "quit", false},

	helpSection("Help menu"),
	{"type", "filter keys by description text", false},
	{"j / k", "move selection in filtered list", false},
	{"g / G", "jump to first / last match", false},
	{"Backspace", "delete last search character", false},
	{"Ctrl+U", "clear search", false},
	{"Esc / ?", "close help", false},

	helpSection("Sidebar"),
	{"j / k", "move tab selection", false},
	{"l / Enter", "focus the right pane on this tab", false},
	{"h", "already on the left pane", false},
	{"", "Overview · Usługi · Devices · Eko · Zaawansowane · Wsparcie · Sesja", false},

	helpSection("Tab content"),
	{"h / Esc", "focus the left pane", false},
	{"l", "move right across tiles", false},
	{"j / k", "navigate the tile grid", false},
	{"Enter", "activate the selected tile", false},
	{"t", "toggle Wi-Fi", false},
	{"g", "toggle guest Wi-Fi", false},
	{"b", "reboot the router", false},
	{"x", "log out and quit (Sesja tab)", false},

	helpSection("Devices"),
	{"j / k", "move selection", false},
	{"h / Esc", "focus the left pane", false},
	{"g / G", "first / last device", false},
	{"Enter / l", "open device detail", false},
	{"y", "copy selected device IP", false},
	{"Y", "copy selected device MAC", false},
	{"Esc", "back to the sidebar", false},

	helpSection("Device detail"),
	{"e", "rename device", false},
	{"T", "change device type", false},
	{"r", "reserve a fixed DHCP IP", false},
	{"y / Y", "copy IP / MAC", false},
	{"Esc / h", "back to the device list", false},

	helpSection("Rename and DHCP"),
	{"Enter", "save", false},
	{"Esc", "cancel", false},

	helpSection("Type and language pickers"),
	{"j / k", "move selection", false},
	{"g / G", "first / last item", false},
	{"Enter", "apply", false},
	{"Esc", "cancel", false},

	helpSection("Confirm"),
	{"y", "confirm reboot or logout", false},
	{"n / Esc", "cancel", false},

	helpSection("Focus"),
	{"", "mauve border is the pane that receives keys", false},
	{"h", "focus the left pane", false},
	{"l", "focus the right pane from the sidebar", false},
}

const helpMarkerW = 3 // "▸  " or "   "
const helpBoxMaxWidth = 96

func helpKeyColWidth() int {
	maxW := 0
	for _, row := range helpRows {
		if row.section {
			continue
		}
		if w := lipgloss.Width(row.key); w > maxW {
			maxW = w
		}
	}
	return helpMarkerW + maxW + 2
}

func (m Model) renderHelpOverlay() string {
	boxW := m.width - 4
	if boxW > helpBoxMaxWidth {
		boxW = helpBoxMaxWidth
	}
	if boxW < 40 {
		boxW = m.width - 2
	}
	if boxW < 20 {
		boxW = 20
	}

	innerW := boxW - 4
	if innerW < 20 {
		innerW = 20
	}
	keyColW := helpKeyColWidth()

	var lines []string
	title := iconHelp + "  Keybindings"
	if m.host != "" {
		title += "  ·  " + m.host
	}
	lines = append(lines, helpInnerLine(helpTitleInBoxStyle.Render(title), innerW))
	lines = append(lines, helpInnerLine(helpHintInBoxStyle.Render("type to search · j/k move · esc or ? close"), innerW))
	if q := strings.TrimSpace(m.helpFilter); q != "" {
		lines = append(lines, helpInnerLine(helpSearchStyle.Render("search: "+q), innerW))
	}
	lines = append(lines, helpInnerLine("", innerW))

	indices := m.helpFilteredIndices()
	if len(indices) == 0 {
		lines = append(lines, helpInnerLine(helpHintInBoxStyle.Render("(no matches)"), innerW))
		box := helpBoxStyle.Width(boxW).Render(strings.Join(lines, "\n"))
		return centerBoxOnPlainScreen(box, m.width, m.height)
	}

	maxRows := m.height - 10
	if m.helpFilter != "" {
		maxRows = m.height - 11
	}
	if maxRows < 6 {
		maxRows = 6
	}
	if maxRows > len(indices) {
		maxRows = len(indices)
	}

	cur := m.clampHelpCursor(m.helpCursor)
	nav := helpNavigableIndices(indices)
	selectedIdx := -1
	if cur >= 0 && cur < len(nav) {
		selectedIdx = nav[cur]
	}
	selDisplay := 0
	for i, idx := range indices {
		if idx == selectedIdx {
			selDisplay = i
			break
		}
	}
	start := 0
	if selDisplay >= maxRows {
		start = selDisplay - maxRows + 1
	}
	end := start + maxRows
	if end > len(indices) {
		end = len(indices)
	}

	for i := start; i < end; i++ {
		rowIdx := indices[i]
		lines = append(lines, renderHelpLine(helpRows[rowIdx], rowIdx == selectedIdx, innerW, keyColW, m.helpFilter))
	}

	box := helpBoxStyle.Width(boxW).Render(strings.Join(lines, "\n"))
	return centerBoxOnPlainScreen(box, m.width, m.height)
}

func helpInnerLine(content string, innerW int) string {
	return fillBarRow(content, innerW, rowPadStyle)
}

func renderHelpLine(row helpRow, selected bool, width, keyColW int, query string) string {
	if row.section {
		title := row.desc
		if lipgloss.Width(title) > width-2 {
			title = truncateRunes(title, max(1, width-2))
		}
		st := helpHintInBoxStyle.Bold(true).Foreground(colorBlue)
		return fillBarRow(st.Render(title), width, rowPadStyle)
	}
	if width < keyColW+4 {
		width = keyColW + 4
	}
	descW := width - keyColW
	if descW < 8 {
		descW = 8
	}
	desc := row.desc
	if lipgloss.Width(desc) > descW {
		desc = truncateRunes(desc, max(1, descW-1)) + "…"
	}
	keyText := row.key
	if lipgloss.Width(keyText) > keyColW-helpMarkerW {
		keyText = truncateRunes(keyText, max(1, keyColW-helpMarkerW-1)) + "…"
	}

	marker := "   "
	if selected {
		marker = "▸  "
	}
	keyPad := padPlainRight(keyText, keyColW-helpMarkerW)

	if selected {
		text := marker + keyPad + desc
		return fillBarRow(helpSelStyle.Render(text), width, helpSelStyle)
	}

	gap := keyColW - helpMarkerW - lipgloss.Width(keyText)
	if gap < 1 {
		gap = 1
	}
	line := helpMarkerStyle.Render(marker) +
		highlightHelpMatch(keyText, query, helpKeyStyle) +
		helpMarkerStyle.Render(strings.Repeat(" ", gap)) +
		highlightHelpMatch(desc, query, helpDescStyle)
	return fillBarRow(line, width, rowPadStyle)
}

func (m Model) helpFilteredIndices() []int {
	return helpFilteredIndices(m.helpFilter)
}

func helpNavigableIndices(indices []int) []int {
	var out []int
	for _, i := range indices {
		if !helpRows[i].section {
			out = append(out, i)
		}
	}
	return out
}

func helpFilteredIndices(query string) []int {
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		out := make([]int, len(helpRows))
		for i := range helpRows {
			out[i] = i
		}
		return out
	}
	tokens := strings.Fields(q)
	var out []int
	for i, row := range helpRows {
		if helpRowMatches(row, tokens) {
			out = append(out, i)
		}
	}
	return out
}

func helpRowMatches(row helpRow, tokens []string) bool {
	if row.section {
		hay := strings.ToLower(row.desc)
		for _, tok := range tokens {
			if strings.Contains(hay, tok) {
				return true
			}
		}
		return false
	}
	hay := strings.ToLower(row.key + " " + row.desc)
	for _, tok := range tokens {
		if !strings.Contains(hay, tok) {
			return false
		}
	}
	return true
}

func helpFilterKey(msg tea.KeyMsg) (string, bool) {
	if msg.Alt {
		return "", false
	}
	switch msg.Type {
	case tea.KeyRunes:
		return string(msg.Runes), true
	case tea.KeySpace:
		return " ", true
	}
	return "", false
}

func helpHighlightToken(query string) string {
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return ""
	}
	return strings.Fields(q)[0]
}

func highlightHelpMatch(text, query string, base lipgloss.Style) string {
	if text == "" {
		return base.Render(text)
	}
	tok := helpHighlightToken(query)
	if tok == "" {
		return base.Render(text)
	}
	lower := strings.ToLower(text)
	idx := strings.Index(lower, tok)
	if idx < 0 {
		return base.Render(text)
	}
	before := text[:idx]
	match := text[idx : idx+len(tok)]
	after := text[idx+len(tok):]
	return base.Render(before) +
		helpMatchStyle.Render(match) +
		base.Render(after)
}

func trimLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
}

func padPlainRight(s string, w int) string {
	if w < 1 {
		return s
	}
	cur := lipgloss.Width(s)
	if cur >= w {
		return s
	}
	return s + strings.Repeat(" ", w-cur)
}

func (m Model) updateHelp(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "?", "q":
		m.showHelp = false
		m.helpFilter = ""
		m.helpCursor = 0
		return m, nil
	case "ctrl+u":
		m.helpFilter = ""
		m.helpCursor = 0
		return m, nil
	case "backspace", "ctrl+h":
		m.helpFilter = trimLastRune(m.helpFilter)
		m.helpCursor = m.clampHelpCursor(m.helpCursor)
		return m, nil
	case "j", "down":
		nav := helpNavigableIndices(m.helpFilteredIndices())
		if len(nav) > 0 {
			m.helpCursor = min(m.helpCursor+1, len(nav)-1)
		}
	case "k", "up":
		if len(helpNavigableIndices(m.helpFilteredIndices())) > 0 {
			m.helpCursor = max(m.helpCursor-1, 0)
		}
	case "g":
		m.helpCursor = 0
	case "G":
		n := len(helpNavigableIndices(m.helpFilteredIndices()))
		if n > 0 {
			m.helpCursor = n - 1
		}
	default:
		if key, ok := helpFilterKey(msg); ok {
			m.helpFilter += key
			if len(helpNavigableIndices(m.helpFilteredIndices())) > 0 {
				m.helpCursor = 0
			}
		}
	}
	return m, nil
}

func (m Model) clampHelpCursor(cur int) int {
	n := len(helpNavigableIndices(m.helpFilteredIndices()))
	if n == 0 {
		return 0
	}
	if cur >= n {
		return n - 1
	}
	if cur < 0 {
		return 0
	}
	return cur
}
