package config

import "testing"

func TestSSIDMatches(t *testing.T) {
	if !ssidMatches("Orange_Swiatlowod_BB98", "orange_swiatlowod_bb98") {
		t.Fatal("expected case-insensitive match")
	}
	if ssidMatches("Other", "Nieszpodzianka") {
		t.Fatal("expected no match")
	}
}

func TestLocationSummary(t *testing.T) {
	summary := locationSummary()
	if len(summary) < 2 {
		t.Fatalf("expected at least 2 active locations, got %v", summary)
	}
}
