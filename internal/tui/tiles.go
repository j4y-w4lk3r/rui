package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// tileState classifies a tile's current state, which drives its color.
type tileState int

const (
	stateNeutral  tileState = iota // unknown / not applicable
	stateOK                        // green ("aktywne", "Connected", "dostępny")
	stateOff                       // red  ("nieaktywna", "niedostępne")
	stateAction                    // orange / actionable (e.g. "press to ...")
)

// tile is one card on a Funbox-style dashboard tab.
type tile struct {
	id       string   // stable id used by key handlers ("wifi", "guest", ...)
	title    string   // Polish label, e.g. "Wi-Fi"
	subtitle string   // status text shown under the title
	state    tileState
	hint     string // bottom-line hint, usually a single-letter binding
}

// tab is one of the top-level Funbox tabs.
type tab struct {
	label string
	id    tabID
}

type tabID int

const (
	tabOverview tabID = iota
	tabServices       // "Usługi" — connected devices, internet, TV, phone, wifi, guest, IoT, history, bandsteer
	tabDevices        // dedicated devices list (Funbox "Połączone urządzenia" expanded)
	tabEko            // "Eko" — power management + scheduling
	tabAdvanced       // "Ustawienia zaawansowane"
	tabSupport        // "Wsparcie"
	tabSession        // "Zarządzanie sesją"
)

var tabs = []tab{
	{"Overview", tabOverview},
	{"Usługi", tabServices},
	{"Devices", tabDevices},
	{"Eko", tabEko},
	{"Zaawansowane", tabAdvanced},
	{"Wsparcie", tabSupport},
	{"Sesja", tabSession},
}

// renderTile draws one tile.
func renderTile(t tile, selected bool) string {
	style := tileBase
	if selected {
		style = tileSel
	}

	var sub string
	switch t.state {
	case stateOK:
		sub = tileOkStyle.Render(t.subtitle)
	case stateOff:
		sub = tileBadStyle.Render(t.subtitle)
	case stateAction:
		sub = okStyle.Render(t.subtitle)
	default:
		sub = tileDimStyle.Render(t.subtitle)
	}

	hint := ""
	if t.hint != "" {
		hint = tileDimStyle.Render(t.hint)
	}

	inner := tileTitleStyle.Render(t.title) + "\n" + sub
	if hint != "" {
		inner += "\n" + hint
	}
	return style.Render(inner)
}

// renderTileGrid arranges tiles row-by-row to fit the given width.
func renderTileGrid(tiles []tile, selectedIdx, widthCols int, availableWidth int) string {
	// Per-tile width including border = 18+2 = 20, plus 1 gap.
	const tileW = 20
	const gap = 1
	cols := widthCols
	if cols < 1 {
		cols = (availableWidth + gap) / (tileW + gap)
		if cols < 1 {
			cols = 1
		}
	}

	var rows []string
	for i := 0; i < len(tiles); i += cols {
		end := i + cols
		if end > len(tiles) {
			end = len(tiles)
		}
		rendered := make([]string, 0, end-i)
		for j := i; j < end; j++ {
			rendered = append(rendered, renderTile(tiles[j], j == selectedIdx))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, rendered...))
	}
	return strings.Join(rows, "\n")
}

// servicesTiles returns the 9 service tiles, populated with live status
// from the model. Where we don't yet have an API answer we fall back to
// "—" / neutral grey.
func (m Model) servicesTiles() []tile {
	devSub := "—"
	devSt := stateNeutral
	if m.devices != nil {
		online := 0
		for _, d := range m.devices {
			if d.Active {
				online++
			}
		}
		devSub = niceCount(online, len(m.devices), "online")
		devSt = stateOK
	}

	internetSub := "—"
	internetSt := stateNeutral
	if m.wan != nil {
		if strings.EqualFold(m.wan.ConnectionState, "Connected") {
			internetSub = "dostępny"
			internetSt = stateOK
		} else {
			internetSub = "niedostępny"
			internetSt = stateOff
		}
	}

	tvSub := "—"
	tvSt := stateNeutral
	if m.iptv != nil {
		if strings.EqualFold(m.iptv.IPTVStatus, "Available") {
			tvSub = "aktywna"
			tvSt = stateOK
		} else {
			tvSub = strings.ToLower(m.iptv.IPTVStatus)
			tvSt = stateOff
		}
	}

	phoneSub := "—"
	phoneSt := stateNeutral
	if m.phone != nil {
		switch {
		case m.phone.Up:
			phoneSub = "aktywny"
			phoneSt = stateOK
		case m.phone.Enabled:
			phoneSub = "włączony"
			phoneSt = stateOK
		default:
			phoneSub = "niedostępne"
			phoneSt = stateOff
		}
	}

	wifiSub := "—"
	wifiSt := stateNeutral
	if m.wifi != nil {
		if m.wifi.Enabled {
			wifiSub = "aktywne"
			wifiSt = stateOK
		} else {
			wifiSub = "nieaktywne"
			wifiSt = stateOff
		}
	}

	guestSub := "—"
	guestSt := stateNeutral
	if m.guest != nil {
		if m.guest.Enable {
			guestSub = "aktywna"
			guestSt = stateOK
		} else {
			guestSub = "nieaktywna"
			guestSt = stateOff
		}
	}

	return []tile{
		{id: "devices", title: "Podłączone urządzenia", subtitle: devSub, state: devSt, hint: "enter"},
		{id: "internet", title: "Internet", subtitle: internetSub, state: internetSt},
		{id: "tv", title: "TV", subtitle: tvSub, state: tvSt},
		{id: "phone", title: "Telefon", subtitle: phoneSub, state: phoneSt},
		{id: "wifi", title: "Wi-Fi", subtitle: wifiSub, state: wifiSt, hint: "t toggle"},
		{id: "guest", title: "Sieć gości", subtitle: guestSub, state: guestSt, hint: "g toggle"},
		{id: "iot", title: "IoT Wi-Fi", subtitle: "nieaktywna", state: stateOff, hint: "n/a"},
		{id: "history", title: "Historia podł.", subtitle: "—", state: stateNeutral, hint: "n/a"},
		{id: "bandsteer", title: "Sterowanie pasmem", subtitle: "—", state: stateNeutral, hint: "n/a"},
	}
}

func (m Model) ekoTiles() []tile {
	led := tile{id: "led", title: "Harmonogram LED", subtitle: "nieaktywne", state: stateOff}
	repeater := tile{id: "repeater", title: "Harmonogram wzm.", subtitle: "nieaktywne", state: stateOff}
	saver := tile{id: "saver", title: "Oszczędz. energii", subtitle: "nieaktywne", state: stateOff}
	wifiSched := tile{id: "wifisched", title: "Harmonogram Wi-Fi", subtitle: "nieaktywny", state: stateOff}

	if m.power != nil {
		if m.power.LEDActive {
			led.subtitle, led.state = "aktywne", stateOK
		}
		if m.power.DeepActive {
			saver.subtitle, saver.state = "aktywny", stateOK
		}
		if m.power.WiFiSchedActive {
			wifiSched.subtitle, wifiSched.state = "aktywny", stateOK
		}
	}
	return []tile{led, repeater, saver, wifiSched}
}

func (m Model) advancedTiles() []tile {
	internet := tile{id: "adv-internet", title: "Połączenie z int.", subtitle: "—", state: stateNeutral}
	if m.wan != nil {
		if strings.EqualFold(m.wan.ConnectionState, "Connected") {
			internet.subtitle, internet.state = "podłączony", stateOK
		} else {
			internet.subtitle, internet.state = "rozłączony", stateOff
		}
	}
	return []tile{
		internet,
		{id: "remote", title: "Mój zdalny dostęp", subtitle: "nieaktywny", state: stateOff},
		{id: "lang", title: "Wybierz język", subtitle: currentLangSubtitle(m), state: stateNeutral, hint: "enter"},
		{id: "network", title: "Sieć", subtitle: "—", state: stateNeutral},
		{id: "firewall", title: "Firewall", subtitle: "—", state: stateNeutral},
		{id: "backup", title: "Kopia konfig.", subtitle: "—", state: stateNeutral},
		{id: "sysinfo", title: "Informacje sys.", subtitle: "—", state: stateNeutral, hint: "enter"},
		{id: "password", title: "Hasło", subtitle: "—", state: stateNeutral},
	}
}

func (m Model) supportTiles() []tile {
	return []tile{
		{id: "tech", title: "Dostęp pomocy", subtitle: "—", state: stateNeutral},
		{id: "speedtest", title: "Pomiar prędkości", subtitle: "—", state: stateNeutral},
		{id: "update", title: "Aktualizacja", subtitle: niceVersion(m), state: stateNeutral},
		{id: "diag", title: "Diagnostyka", subtitle: "—", state: stateNeutral},
		{id: "factory", title: "Ust. fabryczne", subtitle: "—", state: stateOff},
		{id: "restart", title: "Restart", subtitle: "—", state: stateAction, hint: "b reboot"},
	}
}

func (m Model) sessionTiles() []tile {
	return []tile{
		{id: "legal", title: "Nota prawna", subtitle: "—", state: stateNeutral},
		{id: "logout", title: "Wyloguj", subtitle: "—", state: stateAction, hint: "x logout"},
	}
}

func niceCount(active, total int, suffix string) string {
	if total == 0 {
		return "—"
	}
	return itoa(active) + "/" + itoa(total) + " " + suffix
}

func niceVersion(m Model) string {
	if m.info == nil {
		return "—"
	}
	return truncStr(m.info.SoftwareVersion, 14)
}

// currentLangSubtitle prefers the live value reported by the box. If we
// haven't loaded it yet we fall back to "—" so the tile still renders.
func currentLangSubtitle(m Model) string {
	if m.lang != nil && m.lang.Current != "" {
		return strings.ToLower(m.lang.Current)
	}
	return "—"
}

// itoa avoids strconv import bloat.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
