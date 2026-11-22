package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadDefaultProfile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	if err := SaveDefaultProfile("CABIN"); err != nil {
		t.Fatal(err)
	}
	got, err := DefaultProfile()
	if err != nil {
		t.Fatal(err)
	}
	if got != "CABIN" {
		t.Fatalf("DefaultProfile = %q, want CABIN", got)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "rui", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(raw), `"default_profile": "CABIN"`) {
		t.Fatalf("settings.json missing default_profile: %s", raw)
	}
}

func TestResolveStartupProfile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	_ = SaveDefaultProfile("CABIN")

	if got := ResolveStartupProfile("", ""); got != "CABIN" {
		t.Fatalf("saved default = %q, want CABIN", got)
	}
	if got := ResolveStartupProfile("HOME", ""); got != "HOME" {
		t.Fatalf("flag wins = %q, want HOME", got)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || len(s) >= len(sub) && (s == sub || len(s) > 0 && findSub(s, sub))
}

func findSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
