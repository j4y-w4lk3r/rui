package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHelpViewListsKeys(t *testing.T) {
	m := Model{
		showHelp: true,
		width:    100,
		height:   120,
		host:     "192.168.1.1",
	}
	out := m.View()
	for _, want := range []string{
		"Help",
		"Sidebar",
		"?          Toggle",
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

func TestHelpScrollsOnShortTerminal(t *testing.T) {
	m := Model{
		showHelp: true,
		width:    80,
		height:   12,
		host:     "192.168.1.1",
	}
	top := m.View()
	if !strings.Contains(top, "Global") {
		t.Fatalf("expected top of help at scroll 0:\n%s", top)
	}
	if !strings.Contains(top, "scroll") {
		t.Fatalf("expected scroll hint when content overflows:\n%s", top)
	}

	m.helpScroll = m.helpMaxScroll()
	bottom := m.View()
	if strings.Contains(bottom, "Global") {
		t.Fatalf("Global should scroll off screen at max scroll:\n%s", bottom)
	}
	if !strings.Contains(bottom, "Focus") || !strings.Contains(bottom, "192.168.1.1") {
		t.Fatalf("expected bottom sections visible after scroll:\n%s", bottom)
	}
}

func TestHelpScrollKeys(t *testing.T) {
	m := Model{showHelp: true, width: 80, height: 12, host: "192.168.1.1"}
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m2 := next.(Model)
	if m2.helpScroll <= 0 {
		t.Fatal("j should scroll help down")
	}
	next2, _ := m2.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m3 := next2.(Model)
	if m3.helpScroll != 0 {
		t.Fatalf("k should scroll back to top, got scroll=%d", m3.helpScroll)
	}
}
