package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/j4y-w4lk3r/rui/internal/livebox"
)

func key(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestHFocusesSidebarFromDeviceList(t *testing.T) {
	m := Model{focus: focusTab, current: tabDevices, width: 80, height: 24}
	next, _ := m.handleKey(key('h'))
	got := next.(Model)
	if got.focus != focusSidebar {
		t.Fatalf("focus = %v, want sidebar", got.focus)
	}
}

func TestHFocusesSidebarFromGrid(t *testing.T) {
	m := Model{focus: focusTab, current: tabServices, gridIdx: 2, width: 80, height: 24}
	next, _ := m.handleKey(key('h'))
	got := next.(Model)
	if got.focus != focusSidebar {
		t.Fatalf("focus = %v, want sidebar", got.focus)
	}
	if got.gridIdx != 2 {
		t.Fatalf("gridIdx = %d, want 2 (h should not walk tiles)", got.gridIdx)
	}
}

func TestLFocusesRightPaneFromSidebar(t *testing.T) {
	m := Model{focus: focusSidebar, tabIdx: tabIndexOf(tabDevices), width: 80, height: 24}
	next, _ := m.handleKey(key('l'))
	got := next.(Model)
	if got.focus != focusTab {
		t.Fatalf("focus = %v, want tab", got.focus)
	}
	if got.current != tabDevices {
		t.Fatalf("current = %v, want devices", got.current)
	}
}

func TestLeftArrowAtGridEdgeFocusesSidebar(t *testing.T) {
	m := Model{focus: focusTab, current: tabServices, gridIdx: 0, width: 80, height: 24}
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	got := next.(Model)
	if got.focus != focusSidebar {
		t.Fatalf("focus = %v, want sidebar", got.focus)
	}
}

func TestDeviceDetailHReturnsToList(t *testing.T) {
	m := Model{
		focus:     focusTab,
		current:   tabDevices,
		mode:      modeDeviceDetail,
		deviceIdx: 0,
		devices:   []livebox.Device{{Name: "j4ywa0"}},
		width:     80,
		height:    24,
	}
	next, _ := m.handleKey(key('h'))
	got := next.(Model)
	if got.mode != modeNormal {
		t.Fatalf("mode = %v, want device list", got.mode)
	}
	if got.focus != focusTab {
		t.Fatalf("focus = %v, want right pane (detail h returns to the list)", got.focus)
	}
}
