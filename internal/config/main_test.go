package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain: tests never ask the real keyhole for the OpenBao token (it would wait for a touch),
// and read two example sites from a temporary settings.json, never the user's own.
func TestMain(m *testing.M) {
	os.Setenv("RUI_NO_KEYHOLE", "1")
	os.Setenv("RUI_BAO_TOKEN_FILE", os.DevNull+".missing")
	os.Setenv("RUI_BAO_ADDR", "http://127.0.0.1:9") // never reached: there is no token
	dir, err := os.MkdirTemp("", "rui-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "rui"), 0o700); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rui", "settings.json"), []byte(testSites), 0o600); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// writeSettings gives one test its own settings.json.
func writeSettings(t *testing.T, json string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "rui"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rui", "settings.json"), []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
}

// testSites: example sites HOME, CABIN and an inactive OLD.
const testSites = `{
  "sites": [
    {"id": "HOME", "label": "Home", "wifi_ssids": ["MyWiFi"]},
    {"id": "OLD", "label": "Old place", "inactive": true},
    {"id": "CABIN", "label": "Cabin"}
  ]
}`
