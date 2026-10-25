package openbao

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestReadRouter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "test-token" {
			http.Error(w, "no", http.StatusForbidden)
			return
		}
		if r.URL.Path != "/v1/secret/data/rui/plh1" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"data": map[string]string{
				"username":  "admin",
				"password":  "secret",
				"host":      "192.168.1.1",
				"wifi_ssid": "Nieszpodzianka",
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

	got, err := Read("PLH1")
	if err != nil {
		t.Fatal(err)
	}
	if got.User != "admin" || got.Pass != "secret" || got.Host != "192.168.1.1" || got.WiFiSSID != "Nieszpodzianka" {
		t.Fatalf("router = %+v", got)
	}
}
