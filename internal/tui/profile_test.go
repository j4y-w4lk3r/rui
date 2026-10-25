package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestErrorDialogOpensProfilePicker(t *testing.T) {
	m := Model{
		showErrorDialog: true,
		locationLabel:   "Warsaw",
		width:           80,
		height:          24,
	}
	next, cmd := m.handleKey(key('p'))
	got := next.(Model)
	if cmd != nil {
		t.Fatal("opening the picker should not start a login")
	}
	if got.showErrorDialog {
		t.Fatal("error dialog still open")
	}
	if got.mode != modeProfilePick {
		t.Fatalf("mode = %v, want profile picker", got.mode)
	}
	if got.profileIdx != 0 {
		t.Fatalf("profileIdx = %d, want Warsaw first", got.profileIdx)
	}
	view := got.View()
	for _, part := range []string{"PLH1", "PLH3", "Warsaw", "Wrocław"} {
		if !strings.Contains(view, part) {
			t.Fatalf("picker view missing %q:\n%s", part, view)
		}
	}
}

func TestProfilePickerMovesAndCancels(t *testing.T) {
	m := Model{mode: modeProfilePick, width: 80, height: 24}
	next, _ := m.handleKey(key('j'))
	got := next.(Model)
	if got.profileIdx != 1 {
		t.Fatalf("profileIdx = %d, want 1", got.profileIdx)
	}
	cancelled, _ := got.handleKey(tea.KeyMsg{Type: tea.KeyEscape})
	if cancelled.(Model).mode != modeNormal {
		t.Fatal("esc should close the picker")
	}
}

func TestProfilePickerAppliesSelection(t *testing.T) {
	orig := loadProfile
	t.Cleanup(func() { loadProfile = orig })
	loadProfile = func(id string) (string, string, string, string, error) {
		if id != "PLH3" {
			t.Fatalf("loaded %s, want PLH3", id)
		}
		return "192.0.2.1", "user", "secret", "Wrocław", nil
	}

	m := Model{mode: modeProfilePick, profileIdx: 1, width: 80, height: 24, loginGen: 4}
	next, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)
	if cmd == nil {
		t.Fatal("enter should start a login")
	}
	if got.mode != modeNormal {
		t.Fatalf("mode = %v, want normal", got.mode)
	}
	if got.locationLabel != "Wrocław" || got.host != "192.0.2.1" {
		t.Fatalf("label %q host %q", got.locationLabel, got.host)
	}
	if got.loginGen != 5 {
		t.Fatalf("loginGen = %d, want 5", got.loginGen)
	}
	if got.client == nil {
		t.Fatal("client was not replaced")
	}
}

func TestStaleLoginDoesNotReplaceANewerProfile(t *testing.T) {
	m := Model{loginGen: 2, locationLabel: "Wrocław", loading: "Logging in..."}
	next, cmd := m.Update(loginDoneMsg{err: errString("login rejected (HTTP 401)"), gen: 1})
	got := next.(Model)
	if cmd != nil || got.showErrorDialog || got.locationLabel != "Wrocław" {
		t.Fatalf("stale login changed the model: dialog %v label %q", got.showErrorDialog, got.locationLabel)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
