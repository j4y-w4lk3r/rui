package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

// onePasswordProfile maps a short rui profile name to a 1Password login item.
// VaultID is required because vault names may contain characters (e.g. "+")
// that break op:// secret references.
type onePasswordProfile struct {
	VaultID       string
	Item          string
	UsernameField string
	PasswordField string
}

// profiles is the built-in registry of -profile names → 1Password items.
// Add new homes/locations here as you bring them online.
var profiles = map[string]onePasswordProfile{
	"PL": {
		VaultID:       "g3irkmq3taou5ko6gwxwlkcjd4",
		Item:          "PLH2-Orange",
		UsernameField: "username",
		PasswordField: "confirmpassword",
	},
}

// ProfileNames returns the known profile keys for help text.
func ProfileNames() []string {
	out := make([]string, 0, len(profiles))
	for name := range profiles {
		out = append(out, name)
	}
	if len(out) > 1 {
		for i := 0; i < len(out); i++ {
			for j := i + 1; j < len(out); j++ {
				if out[j] < out[i] {
					out[i], out[j] = out[j], out[i]
				}
			}
		}
	}
	return out
}

// ApplyProfile reads router credentials for name from 1Password via the `op`
// CLI and exports them into the process environment (username, password,
// ROUTER_HOST). Call this before Load when -profile is set. godotenv does
// not override existing env vars, so profile values win over any .env file.
func ApplyProfile(name string) error {
	key := strings.ToUpper(strings.TrimSpace(name))
	p, ok := profiles[key]
	if !ok {
		return fmt.Errorf("unknown profile %q (available: %s)", name, strings.Join(ProfileNames(), ", "))
	}

	username, err := opRead(p.opRef(p.UsernameField))
	if err != nil {
		return fmt.Errorf("profile %s: read username: %w", key, err)
	}
	password, err := opRead(p.opRef(p.PasswordField))
	if err != nil {
		return fmt.Errorf("profile %s: read password: %w", key, err)
	}

	host, err := opItemHost(p.Item)
	if err != nil {
		return fmt.Errorf("profile %s: read host: %w", key, err)
	}

	if err := setProfileEnv("username", username); err != nil {
		return err
	}
	if err := setProfileEnv("password", password); err != nil {
		return err
	}
	return setProfileEnv("ROUTER_HOST", host)
}

func (p onePasswordProfile) opRef(field string) string {
	return fmt.Sprintf("op://%s/%s/%s", p.VaultID, p.Item, field)
}

func opRead(reference string) (string, error) {
	cmd := exec.Command("op", "read", reference)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("%s: %s", reference, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("%s: %w", reference, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// opItemHost returns the host portion of the primary URL on a 1Password item.
func opItemHost(item string) (string, error) {
	cmd := exec.Command("op", "item", "get", item, "--format", "json")
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("op item get %q: %s", item, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("op item get %q: %w", item, err)
	}

	var parsed struct {
		URLs []struct {
			Primary bool   `json:"primary"`
			Href    string `json:"href"`
		} `json:"urls"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return "", fmt.Errorf("decode item %q: %w", item, err)
	}

	href := ""
	for _, u := range parsed.URLs {
		if u.Primary && u.Href != "" {
			href = u.Href
			break
		}
	}
	if href == "" && len(parsed.URLs) > 0 {
		href = parsed.URLs[0].Href
	}
	if href == "" {
		return "", fmt.Errorf("item %q has no URL", item)
	}
	return hostFromURL(href)
}

func hostFromURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty URL")
	}
	if !strings.Contains(raw, "://") {
		return strings.TrimRight(raw, "/"), nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Host == "" {
		return "", fmt.Errorf("no host in URL %q", raw)
	}
	return u.Host, nil
}

func setProfileEnv(key, value string) error {
	if value == "" {
		return fmt.Errorf("empty value for %s", key)
	}
	return os.Setenv(key, value)
}
