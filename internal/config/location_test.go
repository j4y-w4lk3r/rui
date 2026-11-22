package config

import (
	"strings"
	"testing"
)

func TestSSIDMatches(t *testing.T) {
	if !ssidMatches("Example_WiFi_AB12", "example_wifi_ab12") {
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

func TestSitesFromSettings(t *testing.T) {
	writeSettings(t, `{"sites":[{"id":"home","label":"Home"},{"id":"x","inactive":true}],
		"gateway_macs":{"HOME":["aa:bb:cc:dd:ee:ff"],"LAB":["11:22:33:44:55:66"]}}`)
	var got []string
	for _, l := range ActiveLocations() {
		got = append(got, l.ID+"="+l.Label)
	}
	if want := "HOME=Home LAB=LAB"; strings.Join(got, " ") != want {
		t.Fatalf("ActiveLocations = %v, want %s (sites first, then gateway_macs IDs; inactive hidden)", got, want)
	}
}

func TestNoSitesNoSettings(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got := ProfileNames(); len(got) != 1 || got[0] != "auto" {
		t.Fatalf("ProfileNames without settings = %v, want [auto]", got)
	}
}
