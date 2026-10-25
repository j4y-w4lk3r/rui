package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHelpViewListsKeys(t *testing.T) {
	m := Model{
		showHelp: true,
		width:    120,
		height:   40,
		host:     "192.168.1.1",
	}
	out := m.View()
	for _, want := range []string{
		"Keybindings",
		"Sidebar",
		"toggle this help menu",
		"type to search",
		"192.168.1.1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help view missing %q", want)
		}
	}
}

func TestHelpToggleKey(t *testing.T) {
	m := Model{width: 80, height: 24}
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m2 := next.(Model)
	if !m2.showHelp {
		t.Fatal("expected showHelp after ?")
	}
	next2, _ := m2.handleKey(tea.KeyMsg{Type: tea.KeyEscape})
	m3 := next2.(Model)
	if m3.showHelp {
		t.Fatal("expected showHelp false after esc")
	}
}

func TestHelpCursorMoves(t *testing.T) {
	m := Model{showHelp: true, width: 100, height: 40, host: "192.168.1.1"}
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m2 := next.(Model)
	if m2.helpCursor != 1 {
		t.Fatalf("j should move help cursor to 1, got %d", m2.helpCursor)
	}
	next2, _ := m2.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m3 := next2.(Model)
	if m3.helpCursor != 0 {
		t.Fatalf("k should move help cursor to 0, got %d", m3.helpCursor)
	}
	next3, _ := m3.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m4 := next3.(Model)
	if m4.helpCursor < 2 {
		t.Fatalf("G should jump to the last match, got %d", m4.helpCursor)
	}
}

func TestHelpFilterTypes(t *testing.T) {
	m := Model{showHelp: true, width: 100, height: 40}
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("guest")})
	m2 := next.(Model)
	if m2.helpFilter != "guest" {
		t.Fatalf("filter = %q", m2.helpFilter)
	}
	out := m2.View()
	if !strings.Contains(out, "search: guest") {
		t.Fatalf("expected search line:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "wi-fi") {
		t.Fatalf("expected wi-fi matches:\n%s", out)
	}
	if strings.Contains(out, "DHCP") {
		t.Fatalf("unrelated DHCP row should be filtered out:\n%s", out)
	}

	next2, _ := m2.handleKey(tea.KeyMsg{Type: tea.KeyCtrlU})
	m3 := next2.(Model)
	if m3.helpFilter != "" {
		t.Fatalf("ctrl+u should clear search, got %q", m3.helpFilter)
	}
}

func TestHelpFilterMatchesDescription(t *testing.T) {
	indices := helpFilteredIndices("reboot")
	if len(indices) == 0 {
		t.Fatal("expected reboot matches")
	}
	found := false
	for _, i := range indices {
		if strings.Contains(strings.ToLower(helpRows[i].desc), "reboot") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("reboot filter did not match description text")
	}
}

func TestHelpFilterNoMatches(t *testing.T) {
	if len(helpFilteredIndices("zzzznotakey")) != 0 {
		t.Fatal("expected no matches")
	}
}

func TestHelpOverlayFitsScreen(t *testing.T) {
	m := Model{showHelp: true, width: 100, height: 24, host: "192.168.1.1"}
	lines := strings.Split(m.View(), "\n")
	if len(lines) != 24 {
		t.Fatalf("rows=%d want 24", len(lines))
	}
}
