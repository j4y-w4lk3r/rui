package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Settings stores durable rui preferences (not secrets).
type Settings struct {
	DefaultProfile string    `json:"default_profile"`
	UpdatedAt      time.Time `json:"updated_at"`
	// GatewayMACs maps a site ID to its router's MAC address(es), for site detection on a cable,
	// e.g. {"PLH1": ["aa:bb:cc:dd:ee:ff"]}. Kept here (per user), never in the code.
	GatewayMACs map[string][]string `json:"gateway_macs,omitempty"`
}

func configDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "rui"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "rui"), nil
}

func settingsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// UserEnvPath is the recommended per-user .env location.
func UserEnvPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".env"), nil
}

// LoadSettings reads ~/.config/rui/settings.json (missing file → zero value).
func LoadSettings() (Settings, error) {
	path, err := settingsPath()
	if err != nil {
		return Settings{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Settings{}, nil
		}
		return Settings{}, err
	}
	var s Settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return Settings{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return s, nil
}

// SaveDefaultProfile remembers which 1Password profile plain `rui` should use.
func SaveDefaultProfile(profile string) error {
	profile = strings.ToUpper(strings.TrimSpace(profile))
	if profile == "" || profile == "AUTO" {
		return nil
	}
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	s, _ := LoadSettings() // keep gateway_macs
	s.DefaultProfile, s.UpdatedAt = profile, time.Now().UTC()
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path, err := settingsPath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

// DefaultProfile returns the saved profile name, e.g. PLH3.
func DefaultProfile() (string, error) {
	s, err := LoadSettings()
	if err != nil {
		return "", err
	}
	return strings.ToUpper(strings.TrimSpace(s.DefaultProfile)), nil
}

// SyncEnvFile writes username/password/ROUTER_HOST from the current process
// environment into the user .env (mode 0600). rui itself no longer calls this
// (credentials from OpenBao / 1Password stay in memory); kept for explicit use.
func SyncEnvFile(path string) error {
	user := strings.TrimSpace(os.Getenv("username"))
	pass := os.Getenv("password")
	host := strings.TrimSpace(getEnv("ROUTER_HOST", "192.168.1.1"))
	if user == "" || pass == "" {
		return fmt.Errorf("cannot sync .env: credentials not in environment")
	}
	if path == "" {
		var err error
		path, err = UserEnvPath()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body := fmt.Sprintf("# Written by rui from 1Password — do not commit\nusername=%s\npassword=%s\nROUTER_HOST=%s\n",
		user, pass, host)
	return os.WriteFile(path, []byte(body), 0o600)
}

// ResolveStartupProfile picks the profile to load before .env.
// Priority: CLI flags > RUI_LOCATION env > saved default > auto (empty string).
func ResolveStartupProfile(flagProfile, flagLocation string) string {
	if p := strings.TrimSpace(flagProfile); p != "" {
		return strings.ToUpper(p)
	}
	if p := strings.TrimSpace(flagLocation); p != "" {
		return strings.ToUpper(p)
	}
	if p := strings.TrimSpace(os.Getenv("RUI_LOCATION")); p != "" {
		return strings.ToUpper(p)
	}
	if p, err := DefaultProfile(); err == nil && p != "" {
		return p
	}
	return ""
}

// BootstrapCredentials loads router creds from 1Password / WiFi before .env.
// Profile resolution order: -profile / -location / RUI_LOCATION → saved default → WiFi auto.
// Explicit -profile always refreshes from 1Password (even if username/password are already
// exported in the shell) and saves the default + ~/.config/rui/.env.
func BootstrapCredentials(flagProfile, flagLocation string) error {
	explicit := strings.TrimSpace(flagProfile) != "" ||
		strings.TrimSpace(flagLocation) != "" ||
		strings.TrimSpace(os.Getenv("RUI_LOCATION")) != ""

	prof := ResolveStartupProfile(flagProfile, flagLocation)

	// Not told explicitly: where we are (WiFi or the router's MAC) beats the saved default.
	if !explicit && os.Getenv("username") == "" {
		if loc, how, err := DetectAndApplyProfile(); err == nil {
			_ = SaveDefaultProfile(loc.ID)
			fmt.Fprintf(os.Stderr, "rui: location %s (%s) from %s\n", loc.ID, loc.Label, how)
			return nil
		}
	}

	if prof != "" {
		saved, _ := DefaultProfile()
		fromSaved := !explicit && strings.EqualFold(prof, saved) && saved != ""

		if err := ApplyProfile(prof); err != nil {
			if fromSaved {
				fmt.Fprintf(os.Stderr, "rui: saved default profile %s failed: %v\n", prof, err)
				fmt.Fprintln(os.Stderr, "rui: tip: run  rui -profile PLH3  to refresh from OpenBao or 1Password")
			} else {
				return fmt.Errorf("profile %s: %w", prof, err)
			}
		} else {
			if explicit {
				if err := SaveDefaultProfile(prof); err != nil {
					fmt.Fprintf(os.Stderr, "rui: warning: could not save default profile: %v\n", err)
				}
				fmt.Fprintf(os.Stderr, "rui: profile %s saved as default (credentials stay in memory)\n", prof)
			} else {
				fmt.Fprintf(os.Stderr, "rui: default profile %s\n", prof)
			}
			return nil
		}
	}

	if os.Getenv("username") != "" && os.Getenv("password") != "" {
		return nil
	}

	loc, ssid, err := DetectAndApplyProfile()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rui: auto-location skipped: %v\n", err)
		fmt.Fprintln(os.Stderr, "rui: tip: use  rui -location PLH3, or add the router MAC to gateway_macs in ~/.config/rui/settings.json")
		if _, statErr := os.Stat(mustUserEnvPath()); statErr == nil {
			fmt.Fprintln(os.Stderr, "rui: warning: falling back to ~/.config/rui/.env — if login fails, use -profile PLH3")
		}
		return nil
	}
	_ = SaveDefaultProfile(loc.ID)
	fmt.Fprintf(os.Stderr, "rui: location %s (%s) from %s\n", loc.ID, loc.Label, ssid)
	return nil
}

func mustUserEnvPath() string {
	p, err := UserEnvPath()
	if err != nil {
		return ""
	}
	return p
}
