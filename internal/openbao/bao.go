// Package openbao reads one router login from OpenBao KV v2.
//
// Items live at secret/rui/plh1, secret/rui/plh3, … with username, password,
// and host. wifi_ssid is optional. The token file can read that path only.
package openbao

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Router is one Livebox login.
type Router struct {
	User     string
	Pass     string
	Host     string
	WiFiSSID string
}

// Configured reports whether the rui OpenBao token file is present.
func Configured() bool {
	_, err := os.Stat(tokenPath())
	return err == nil
}

// Read loads secret/rui/<id>. id is a profile name such as plh1 or pl.
func Read(id string) (Router, error) {
	name := strings.ToLower(strings.TrimSpace(id))
	if name == "" {
		return Router{}, fmt.Errorf("empty OpenBao item name")
	}
	addr, err := address()
	if err != nil {
		return Router{}, err
	}
	token, err := token()
	if err != nil {
		return Router{}, err
	}
	var body struct {
		Data struct {
			Data map[string]string `json:"data"`
		} `json:"data"`
	}
	url := addr + "/v1/secret/data/rui/" + name
	if err := getJSON(url, token, &body); err != nil {
		return Router{}, fmt.Errorf("secret/rui/%s: %w", name, err)
	}
	item := body.Data.Data
	user := strings.TrimSpace(item["username"])
	pass := item["password"]
	host := strings.TrimSpace(item["host"])
	if user == "" || pass == "" || host == "" {
		return Router{}, fmt.Errorf("secret/rui/%s is missing username, password, or host", name)
	}
	return Router{
		User:     user,
		Pass:     pass,
		Host:     host,
		WiFiSSID: strings.TrimSpace(item["wifi_ssid"]),
	}, nil
}

func address() (string, error) {
	if v := strings.TrimSpace(os.Getenv("RUI_BAO_ADDR")); v != "" {
		return strings.TrimRight(v, "/"), nil
	}
	text, err := os.ReadFile(configPath("openbao.addr"))
	if err != nil {
		return "", fmt.Errorf("OpenBao address missing (set RUI_BAO_ADDR or ~/.config/j4y/openbao.addr)")
	}
	addr := strings.TrimSpace(string(text))
	if addr == "" {
		return "", fmt.Errorf("OpenBao address file is empty")
	}
	return strings.TrimRight(addr, "/"), nil
}

func token() (string, error) {
	path := tokenPath()
	text, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("OpenBao token missing (%s)", path)
	}
	tok := strings.TrimSpace(string(text))
	if tok == "" {
		return "", fmt.Errorf("OpenBao token file is empty")
	}
	return tok, nil
}

func tokenPath() string {
	if path := os.Getenv("RUI_BAO_TOKEN_FILE"); path != "" {
		return path
	}
	return configPath("openbao-rui.token")
}

func configPath(name string) string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "j4y", name)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "j4y", name)
	}
	return filepath.Join(home, ".config", "j4y", name)
}

func getJSON(url, token string, dest any) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", token)
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("OpenBao request failed: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("OpenBao returned %d", res.StatusCode)
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("OpenBao reply was not understood")
	}
	return nil
}
