package config

import "testing"

func TestSSIDMatches(t *testing.T) {
	if !ssidMatches("Example_WiFi_AB12", "Example_WiFi_AB12") {
		t.Fatal("expected case-insensitive match")
	}
	if ssidMatches("Other", "MyWiFi") {
		t.Fatal("expected no match")
	}
}

func TestLocationSummary(t *testing.T) {
	summary := locationSummary()
	if len(summary) < 2 {
		t.Fatalf("expected at least 2 active locations, got %v", summary)
	}
}
