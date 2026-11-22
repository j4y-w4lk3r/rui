// Package openbao reads one router login from OpenBao KV v2.
//
// Items live at secret/rui/<site id> (lower case) with username, password,
// and host. wifi_ssid is optional. The token (policy rui) can read that path only.
//
// The token comes from, first hit wins:
//  1. $RUI_BAO_TOKEN
//  2. keyhole entry "openbao-rui": the local keyhole daemon (`keyhole ask`), else
//     `keyhole get VAULT -i IDENTITY` (one touch); nothing on disk. VAULT/IDENTITY come from
//     $RUI_KEYHOLE_VAULT / $RUI_KEYHOLE_ID or settings.json (keyhole_vault, keyhole_identity)
//  3. ~/.config/j4y/openbao-rui.token (or $RUI_BAO_TOKEN_FILE): only on machines without
//     keyhole (servers), renewed there by a timer
package openbao

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// KeyholeEntry is the keyhole entry that holds the rui OpenBao token.
const KeyholeEntry = "openbao-rui"

var (
	tokOnce  sync.Once
	tokValue string
	tokFrom  string
	tokErr   error

	renewOnce sync.Once

	// keyholeCmds are tried in order for the token (tests replace them).
	keyholeCmds = defaultKeyholeCmds
)

// Router is one Livebox login.
type Router struct {
	User     string
	Pass     string
	Host     string
	WiFiSSID string
}

// Configured reports whether a rui OpenBao token is available (environment,
// keyhole, or the token file). The lookup runs once per process.
func Configured() bool {
	_, err := token()
	return err == nil
}

// TokenSource says where the token came from ("keyhole openbao-rui", the file
// path, …), for messages; empty when there is none.
func TokenSource() string {
	_, _ = token()
	return tokFrom
}

// Read loads secret/rui/<id>. id is a site ID such as home.
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
		return Router{}, fmt.Errorf("secret/rui/%s (token from %s): %w", name, tokFrom, err)
	}
	renew(addr, token)
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

// renew extends the token's period once per run (best effort), so a token that
// is only kept in keyhole stays alive as long as rui is used.
func renew(addr, tok string) {
	renewOnce.Do(func() {
		req, err := http.NewRequest(http.MethodPost, addr+"/v1/auth/token/renew-self", nil)
		if err != nil {
			return
		}
		req.Header.Set("X-Vault-Token", tok)
		client := &http.Client{Timeout: 10 * time.Second}
		if res, err := client.Do(req); err == nil {
			res.Body.Close()
		}
	})
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
	tokOnce.Do(func() { tokValue, tokFrom, tokErr = findToken() })
	return tokValue, tokErr
}

func findToken() (string, string, error) {
	if v := strings.TrimSpace(os.Getenv("RUI_BAO_TOKEN")); v != "" {
		return v, "$RUI_BAO_TOKEN", nil
	}
	// keyhole: the local daemon first (no touch while unlocked), then the vault itself (one touch)
	for _, args := range keyholeCmds() {
		if v := run(args...); len(v) >= 20 {
			return v, "keyhole " + KeyholeEntry, nil
		}
	}
	path := tokenPath()
	if text, err := os.ReadFile(path); err == nil {
		if tok := strings.TrimSpace(string(text)); tok != "" {
			return tok, path, nil
		}
	}
	return "", "", fmt.Errorf("no OpenBao token (keyhole %s not readable: daemon not running, or the vault not there; no %s)", KeyholeEntry, path)
}

// defaultKeyholeCmds: `keyhole ask` (the local daemon), then `keyhole get VAULT -i IDENTITY`
// when both files are there (the stick is in). VAULT and IDENTITY are per user, never in the
// code: $RUI_KEYHOLE_VAULT / $RUI_KEYHOLE_ID, else "keyhole_vault" / "keyhole_identity" in
// ~/.config/rui/settings.json.
func defaultKeyholeCmds() [][]string {
	if os.Getenv("RUI_NO_KEYHOLE") != "" { // tests: never touch the real keyhole
		return nil
	}
	cmds := [][]string{{"keyhole", "ask", KeyholeEntry}}
	vault, id := os.Getenv("RUI_KEYHOLE_VAULT"), os.Getenv("RUI_KEYHOLE_ID")
	if vault == "" || id == "" {
		sv, si := keyholeSettings()
		if vault == "" {
			vault = sv
		}
		if id == "" {
			id = si
		}
	}
	if vault == "" || id == "" {
		return cmds
	}
	if _, err := os.Stat(vault); err == nil {
		if _, err := os.Stat(id); err == nil {
			cmds = append(cmds, []string{"keyhole", "get", vault, "-i", id, KeyholeEntry})
		}
	}
	return cmds
}

// keyholeSettings reads keyhole_vault / keyhole_identity from ~/.config/rui/settings.json
// ($XDG_CONFIG_HOME respected); a leading ~/ means the home directory.
func keyholeSettings() (vault, id string) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	home, _ := os.UserHomeDir()
	if dir == "" {
		dir = filepath.Join(home, ".config")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "rui", "settings.json"))
	if err != nil {
		return "", ""
	}
	var s struct {
		Vault    string `json:"keyhole_vault"`
		Identity string `json:"keyhole_identity"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return "", ""
	}
	expand := func(p string) string {
		if strings.HasPrefix(p, "~/") {
			return filepath.Join(home, p[2:])
		}
		return p
	}
	return expand(s.Vault), expand(s.Identity)
}

// run returns a command's stdout, or "" if it is not installed or fails. stdin is
// empty. keyhole get's stderr is shown (its "touch your token" prompt; it also prints
// the entry's username, which is not secret); keyhole ask's is dropped.
func run(args ...string) string {
	if _, err := exec.LookPath(args[0]); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	if len(args) > 1 && args[1] == "get" {
		fmt.Fprintln(os.Stderr, "rui: OpenBao token from keyhole: touch the key")
		cmd.Stderr = os.Stderr
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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
