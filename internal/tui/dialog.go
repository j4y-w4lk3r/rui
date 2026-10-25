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
			"Wait for the timer, then press p and choose the other house\n" +
			"only if this machine is plugged into that Livebox."
		hint = "p choose location  •  r retry  •  Enter dismiss"
	case strings.Contains(lower, "401") || strings.Contains(lower, "invalid") || strings.Contains(lower, "credential"):
		title = "Login rejected"
		body = "This Livebox refused the saved profile.\n\n" +
			"Warsaw and Wrocław both answer at 192.168.1.1.\n" +
			"Press p and choose the house this machine is on."
		hint = "p choose location  •  r retry  •  Enter dismiss"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline"):
		title = "Router unreachable"
		body = "Could not reach the Livebox in time.\n\n" +
			"• Are you on the home WiFi / LAN?\n" +
			"• Router admin is usually http://192.168.1.1"
		hint = "p choose location  •  r retry  •  Enter dismiss"
	case strings.Contains(lower, "connection refused") || strings.Contains(lower, "no such host"):
		title = "Cannot reach router"
		body = "No HTTP response from the router host.\n\n" +
			"rui talks to the Orange box on your LAN — not via PiKVM.\n" +
			"Join the location WiFi first, then run rui again."
		hint = "p choose location  •  Enter dismiss"
	default:
		title = "Login failed"
		body = raw
		hint = "p choose location  •  r retry  •  Enter dismiss"
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
