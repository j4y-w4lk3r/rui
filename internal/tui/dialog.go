package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func friendlyLoginError(err error) (title, body, hint string) {
	if err == nil {
		return "Connection failed", "Unknown error.", "Press Enter to dismiss, then r to retry."
	}
	raw := err.Error()
	lower := strings.ToLower(raw)

	switch {
	case strings.Contains(lower, "failed login attempts") || strings.Contains(lower, "waittime"):
		title = "Router login locked"
		body = "Too many wrong password attempts.\n\n" +
			"The Orange box temporarily blocks admin login.\n" +
			"Wait for the timer, then run:\n\n" +
			"  rui -profile PLH3\n\n" +
			"Do not use plain `rui` (old ~/.config/rui/.env) while testing."
		hint = "Press Enter or Esc  •  wait ~30s  •  r retry"
	case strings.Contains(lower, "401") || strings.Contains(lower, "invalid") || strings.Contains(lower, "credential"):
		title = "Login rejected"
		body = "The router refused these credentials.\n\n" +
			"• Wrong password for this location?\n" +
			"• Try: rui -profile auto  (pick via WiFi)\n" +
			"• Or:  rui -profile PLH3  (force a site)"
		hint = "Press Enter or Esc to dismiss  •  r retry after fixing 1Password / .env"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline"):
		title = "Router unreachable"
		body = "Could not reach the Livebox in time.\n\n" +
			"• Are you on the home WiFi / LAN?\n" +
			"• Router admin is usually http://192.168.1.1"
		hint = "Press Enter or Esc to dismiss  •  check network, then r"
	case strings.Contains(lower, "connection refused") || strings.Contains(lower, "no such host"):
		title = "Cannot reach router"
		body = "No HTTP response from the router host.\n\n" +
			"rui talks to the Orange box on your LAN — not via PiKVM.\n" +
			"Join the location WiFi first, then run rui again."
		hint = "Press Enter or Esc to dismiss"
	default:
		title = "Login failed"
		body = raw
		hint = "Press Enter or Esc to dismiss  •  r retry"
	}
	return title, body, hint
}

func (m Model) renderErrorDialog() string {
	if !m.showErrorDialog {
		return ""
	}

	icon := badStyle.Render(iconAlert + "  ")
	title := badStyle.Render(m.errorTitle)
	body := valStyle.Render(m.errorBody)
	hint := hintStyle.Render(m.errorHint)

	inner := lipgloss.JoinVertical(lipgloss.Left,
		icon+title,
		"",
		body,
		"",
		hint,
	)

	boxW := m.width - 8
	if boxW > 64 {
		boxW = 64
	}
	if boxW < 40 {
		boxW = m.width - 4
		if boxW < 30 {
			boxW = 30
		}
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBad).
		Padding(1, 2).
		Width(boxW).
		Render(inner)

	return centerBoxOnPlainScreen(box, m.width, m.height)
}
