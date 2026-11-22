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
	// Sites: the router sites, e.g. [{"id": "HOME", "label": "Home", "wifi_ssids": ["MyWiFi"]}].
	// Kept here (per user), never in the code. Each site's login is OpenBao secret/rui/<id>.
	Sites []Site `json:"sites,omitempty"`
	// GatewayMACs maps a site ID to its router's MAC address(es), for site detection on a cable,
	// e.g. {"HOME": ["aa:bb:cc:dd:ee:ff"]}. Kept here (per user), never in the code.
	GatewayMACs map[string][]string `json:"gateway_macs,omitempty"`
	// KeyholeVault / KeyholeIdentity: where `keyhole get` finds the vault and the age identity
	// for the OpenBao token (read by internal/openbao; kept here so SaveDefaultProfile keeps them).
	KeyholeVault    string `json:"keyhole_vault,omitempty"`
	KeyholeIdentity string `json:"keyhole_identity,omitempty"`
}

// Site is one entry of settings.json "sites".
type Site struct {
	ID        string   `json:"id"`
	Label     string   `json:"label,omitempty"`
	WiFiSSIDs []string `json:"wifi_ssids,omitempty"`
	Inactive  bool     `json:"inactive,omitempty"`
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

// SaveDefaultProfile remembers which site plain `rui` should use.
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
	s, _ := LoadSettings() // keep sites, gateway_macs, keyhole paths
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

// DefaultProfile returns the saved profile name, e.g. HOME.
func DefaultProfile() (string, error) {
	s, err := LoadSettings()
	if err != nil {
		return "", err
	}
	return strings.ToUpper(strings.TrimSpace(s.DefaultProfile)), nil
}

// ResolveStartupProfile picks the profile to load.
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

// BootstrapCredentials loads router creds from OpenBao into the process environment (memory only).
// Profile resolution order: -profile / -location / RUI_LOCATION → saved default → WiFi auto.
// Explicit -profile always refreshes (even if username/password are already exported in the
// shell) and saves only the profile name as the default (settings.json; no secrets).
func BootstrapCredentials(flagProfile, flagLocation string) error {
	explicit := strings.TrimSpace(flagProfile) != "" ||
		strings.TrimSpace(flagLocation) != "" ||
		strings.TrimSpace(os.Getenv("RUI_LOCATION")) != ""

	prof := ResolveStartupProfile(flagProfile, flagLocation)

	// Not told explicitly: where we are (WiFi or the router's MAC) beats the saved default.
	if !explicit && os.Getenv("username") == "" {
		if loc, how, derr := DetectLocation(); derr == nil {
			// The site is known: use its login or stop. Never fall back to another site's login
			// (22/11/2026: a failed load fell back to the saved default and sent the wrong site's login).
			if err := ApplyLocation(loc); err != nil {
				return fmt.Errorf("site %s (%s, from %s): %w", loc.ID, loc.Label, how, err)
			}
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
				fmt.Fprintln(os.Stderr, "rui: tip: run  rui -profile <site ID>  to load it from OpenBao")
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
		fmt.Fprintln(os.Stderr, "rui: tip: use  rui -location <site ID>, or add the router MAC to gateway_macs in ~/.config/rui/settings.json")
		return nil
	}
	_ = SaveDefaultProfile(loc.ID)
	fmt.Fprintf(os.Stderr, "rui: location %s (%s) from %s\n", loc.ID, loc.Label, ssid)
	return nil
}
