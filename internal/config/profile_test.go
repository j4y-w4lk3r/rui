package config

import (
	"strings"
	"testing"
)

func TestProfileNamesAreSites(t *testing.T) {
	got := strings.Join(ProfileNames(), ",")
	if got != "auto,HOME,CABIN" {
		t.Fatalf("ProfileNames = %s, want auto,HOME,CABIN (active sites only, no 1Password profiles)", got)
	}
}

func TestApplyProfileUnknown(t *testing.T) {
	if err := ApplyProfile("PL"); err == nil || !strings.Contains(err.Error(), "unknown profile") {
		t.Fatalf("ApplyProfile(PL) = %v, want unknown profile (1Password profiles are gone)", err)
	}
}

func TestApplyLocationNeedsOpenBao(t *testing.T) {
	// TestMain: no keyhole, no token file -> a clear error, never a fallback
	t.Setenv("RUI_BAO_TOKEN", "")
	err := ApplyProfile("HOME")
	if err == nil || !strings.Contains(err.Error(), "no OpenBao token") {
		t.Fatalf("ApplyProfile(HOME) without a token = %v, want 'no OpenBao token'", err)
	}
}
