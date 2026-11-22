package openbao

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// resetToken forgets the cached token and replaces the keyhole commands, so tests never
// touch the real keyhole.
func TestMain(m *testing.M) {
	os.Setenv("RUI_NO_KEYHOLE", "1")
	os.Exit(m.Run())
}

func resetToken(t *testing.T, cmds [][]string) {
	t.Helper()
	old := keyholeCmds
	keyholeCmds = func() [][]string { return cmds }
	tokOnce, tokValue, tokFrom, tokErr = sync.Once{}, "", "", nil
	t.Cleanup(func() {
		keyholeCmds = old
		tokOnce, tokValue, tokFrom, tokErr = sync.Once{}, "", "", nil
	})
}

func TestTokenOrder(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "token")
	if err := os.WriteFile(file, []byte("file-token-0123456789\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUI_BAO_TOKEN_FILE", file)
	t.Setenv("RUI_BAO_TOKEN", "")

	// keyhole answers -> keyhole wins over the file
	resetToken(t, [][]string{{"echo", "keyhole-token-0123456789"}})
	if tok, _ := token(); tok != "keyhole-token-0123456789" || TokenSource() != "keyhole openbao-rui" {
		t.Fatalf("keyhole: got %q from %q", tok, TokenSource())
	}
	// keyhole silent (locked / not installed) -> the file
	resetToken(t, [][]string{{"false"}, {"no-such-command-rui"}})
	if tok, _ := token(); tok != "file-token-0123456789" || TokenSource() != file {
		t.Fatalf("file: got %q from %q", tok, TokenSource())
	}
	// nothing at all -> not configured
	resetToken(t, nil)
	t.Setenv("RUI_BAO_TOKEN_FILE", filepath.Join(dir, "missing"))
	if Configured() {
		t.Fatal("Configured() with no token source")
	}
	// environment beats everything
	resetToken(t, [][]string{{"echo", "keyhole-token-0123456789"}})
	t.Setenv("RUI_BAO_TOKEN", "env-token")
	if tok, _ := token(); tok != "env-token" {
		t.Fatalf("env: got %q", tok)
	}
}

func TestReadRouter(t *testing.T) {
	resetToken(t, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "test-token" {
			http.Error(w, "no", http.StatusForbidden)
			return
		}
		if r.URL.Path != "/v1/secret/data/rui/home" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"data": map[string]string{
				"username":  "admin",
				"password":  "secret",
				"host":      "192.168.1.1",
				"wifi_ssid": "MyWiFi",
			}},
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUI_BAO_ADDR", srv.URL)
	t.Setenv("RUI_BAO_TOKEN_FILE", tokenFile)

	got, err := Read("HOME")
	if err != nil {
		t.Fatal(err)
	}
	if got.User != "admin" || got.Pass != "secret" || got.Host != "192.168.1.1" || got.WiFiSSID != "MyWiFi" {
		t.Fatalf("router = %+v", got)
	}
}

func TestKeyholeSettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("RUI_NO_KEYHOLE", "")
	t.Setenv("RUI_KEYHOLE_VAULT", "")
	t.Setenv("RUI_KEYHOLE_ID", "")
	vault, id := filepath.Join(dir, "v.age"), filepath.Join(dir, "id.txt")
	for _, f := range []string{vault, id} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "rui"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := `{"keyhole_vault": "~/v.age", "keyhole_identity": "` + id + `"}`
	if err := os.WriteFile(filepath.Join(dir, "rui", "settings.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	cmds := defaultKeyholeCmds()
	if len(cmds) != 2 || cmds[1][2] != vault || cmds[1][4] != id {
		t.Fatalf("cmds = %v, want keyhole ask + keyhole get %s -i %s", cmds, vault, id)
	}
	// no settings, no env: only the daemon, never a guessed path
	os.Remove(filepath.Join(dir, "rui", "settings.json"))
	if cmds := defaultKeyholeCmds(); len(cmds) != 1 {
		t.Fatalf("without settings cmds = %v, want only keyhole ask", cmds)
	}
}
