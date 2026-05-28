package tui

import (
	"context"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/j4y-w4lk3r/rui/internal/livebox"
)

// Message types pushed back into Update by the various tea.Cmd helpers.
type (
	loginDoneMsg   struct{ err error }
	deviceInfoMsg  struct {
		info *livebox.DeviceInfo
		err  error
	}
	wanStatusMsg struct {
		status *livebox.WANStatus
		err    error
	}
	devicesMsg struct {
		devices []livebox.Device
		err     error
	}
	wifiStatusMsg struct {
		status *livebox.WiFiStatus
		err    error
	}
	guestWiFiMsg struct {
		status *livebox.GuestWiFiStatus
		err    error
	}
	iptvMsg struct {
		status *livebox.IPTVStatus
		err    error
	}
	phoneMsg struct {
		status *livebox.PhoneStatus
		err    error
	}
	powerMsg struct {
		status *livebox.PowerProfileSummary
		err    error
	}
	languageMsg struct {
		info *livebox.LanguageInfo
		err  error
	}
	actionDoneMsg struct {
		label string
		err   error
	}
	// clipboardMsg is like actionDoneMsg but its handler MUST NOT
	// refresh the current tab. Copying a string to the OS clipboard
	// has no router-side effect, so refreshing would just throw away
	// the "Copied IP …" status the user just earned.
	clipboardMsg struct {
		label string
		err   error
	}
	tickMsg time.Time
)

const callTimeout = 8 * time.Second

func loginCmd(c *livebox.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		return loginDoneMsg{err: c.Login(ctx)}
	}
}

func fetchDeviceInfo(c *livebox.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		info, err := c.GetDeviceInfo(ctx)
		return deviceInfoMsg{info: info, err: err}
	}
}

func fetchWAN(c *livebox.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		s, err := c.GetWANStatus(ctx)
		return wanStatusMsg{status: s, err: err}
	}
}

func fetchDevices(c *livebox.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		d, err := c.ListDevices(ctx)
		return devicesMsg{devices: d, err: err}
	}
}

func fetchWiFi(c *livebox.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		s, err := c.GetWiFiStatus(ctx)
		return wifiStatusMsg{status: s, err: err}
	}
}

func setWiFiCmd(c *livebox.Client, enable bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		label := "Wi-Fi enabled"
		if !enable {
			label = "Wi-Fi disabled"
		}
		return actionDoneMsg{label: label, err: c.SetWiFi(ctx, enable)}
	}
}

func fetchGuestWiFi(c *livebox.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		s, err := c.GetGuestWiFi(ctx)
		return guestWiFiMsg{status: s, err: err}
	}
}

func setGuestWiFiCmd(c *livebox.Client, enable bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		label := "Guest Wi-Fi enabled"
		if !enable {
			label = "Guest Wi-Fi disabled"
		}
		return actionDoneMsg{label: label, err: c.SetGuestWiFi(ctx, enable)}
	}
}

func rebootCmd(c *livebox.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		return actionDoneMsg{label: "Reboot requested", err: c.Reboot(ctx)}
	}
}

func logoutCmd(c *livebox.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		return actionDoneMsg{label: "Logout", err: c.Logout(ctx)}
	}
}

func fetchIPTV(c *livebox.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		s, err := c.GetIPTV(ctx)
		return iptvMsg{status: s, err: err}
	}
}

func fetchPhone(c *livebox.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		s, err := c.GetPhone(ctx)
		return phoneMsg{status: s, err: err}
	}
}

func fetchPower(c *livebox.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		s, err := c.GetPowerProfiles(ctx)
		return powerMsg{status: s, err: err}
	}
}

func renameDeviceCmd(c *livebox.Client, key, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		return actionDoneMsg{
			label: "Renamed to " + name,
			err:   c.RenameDevice(ctx, key, name),
		}
	}
}

func fetchLanguage(c *livebox.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		info, err := c.GetLanguage(ctx)
		return languageMsg{info: info, err: err}
	}
}

func setLanguageCmd(c *livebox.Client, lang string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		return actionDoneMsg{
			label: "Language set to " + lang,
			err:   c.SetLanguage(ctx, lang),
		}
	}
}

func setDeviceTypeCmd(c *livebox.Client, key, dtype string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		return actionDoneMsg{
			label: "Type set to " + dtype,
			err:   c.SetDeviceType(ctx, key, dtype),
		}
	}
}

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// copyToClipboardCmd writes `text` to the system clipboard. We reuse
// the existing actionDoneMsg pipeline so the result lands in the
// status bar like every other one-shot action. `what` is shown to the
// user (e.g. "IP 192.168.1.14") so they know what was just yanked.
//
// We deliberately do nothing on an empty string — silently copying ""
// would just clear the clipboard, which is almost never what the user
// wanted.
func copyToClipboardCmd(text, what string) tea.Cmd {
	return func() tea.Msg {
		if text == "" {
			return clipboardMsg{label: "Copy " + what + " (empty value)", err: nil}
		}
		err := clipboard.WriteAll(text)
		return clipboardMsg{label: "Copied " + what + ": " + text, err: err}
	}
}
