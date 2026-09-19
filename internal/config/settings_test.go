package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadDefaultProfile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	if err := SaveDefaultProfile("PLH3"); err != nil {
		t.Fatal(err)
	}
	got, err := DefaultProfile()
	if err != nil {
		t.Fatal(err)
	}
	if got != "PLH3" {
		t.Fatalf("DefaultProfile = %q, want PLH3", got)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "rui", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(raw), `"default_profile": "PLH3"`) {
		t.Fatalf("settings.json missing default_profile: %s", raw)
	}
}

func TestResolveStartupProfile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	_ = SaveDefaultProfile("PLH3")

	if got := ResolveStartupProfile("", ""); got != "PLH3" {
		t.Fatalf("saved default = %q, want PLH3", got)
	}
	if got := ResolveStartupProfile("PLH1", ""); got != "PLH1" {
		t.Fatalf("flag wins = %q, want PLH1", got)
	}
}

func TestSyncEnvFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("username", "admin")
	t.Setenv("password", "secret")
	t.Setenv("ROUTER_HOST", "192.168.1.1")

	if err := SyncEnvFile(""); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rui", ".env")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !contains(body, "username=admin") || !contains(body, "password=secret") {
		t.Fatalf(".env content: %s", body)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf(".env mode = %o, want 0600", info.Mode().Perm())
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
