package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/j4y-w4lk3r/rui/internal/openbao"
)

// Location describes one Orange Livebox home. Its login lives in OpenBao secret/rui/<id>
// (lower case). WiFiSSIDs (plus the optional wifi_ssid in OpenBao) and the router's MAC
// (settings.json gateway_macs) pick the site when rui starts on that LAN.
type Location struct {
	ID        string
	Label     string
	WiFiSSIDs []string
	Active    bool
}

var locations = []Location{
	{
		ID:        "HOME",
		Label:     "Home",
		WiFiSSIDs: []string{"MyWiFi"},
		Active:    true,
	},
	{
		ID:        "OLD",
		Label:     "Location 3",
		WiFiSSIDs: []string{"OtherWiFi"},
		Active:    false,
	},
	{
		ID:     "CABIN",
		Label:  "Cabin",
		Active: true,
	},
}

// LocationNames returns active location IDs for help text.
func LocationNames() []string {
	var out []string
	for _, loc := range ActiveLocations() {
		out = append(out, loc.ID)
	}
	return out
}

// ActiveLocations returns the sites the TUI can switch to.
func ActiveLocations() []Location {
	var out []Location
	for _, loc := range locations {
		if loc.Active {
			out = append(out, loc)
		}
	}
	return out
}

// DetectLocation finds the site: the WiFi name first, then the router's MAC address
// (works on a cable and on macOS). The second value says how it was found.
func DetectLocation() (Location, string, error) {
	ssid, werr := currentWiFiSSID()
	if werr != nil || ssid == "" {
		if loc, how, err := locationFromGateway(); err == nil {
			return loc, how, nil
		} else if werr != nil {
			return Location{}, "", fmt.Errorf("%v; %v", werr, err)
		} else {
			return Location{}, "", fmt.Errorf("not on WiFi and %v", err)
		}
	}

	for _, loc := range locations {
		if !loc.Active {
			continue
		}
		for _, want := range loc.wifiFingerprints() {
			if ssidMatches(ssid, want) {
				return loc, fmt.Sprintf("WiFi %q", ssid), nil
			}
		}
	}
	return Location{}, ssid, fmt.Errorf("unknown location for WiFi %q (known: %s)", ssid, strings.Join(locationSummary(), "; "))
}

// ApplyLocation loads router credentials for loc from OpenBao (token from keyhole, or the
// token file where keyhole does not run yet). Nothing is written to disk.
func ApplyLocation(loc Location) error {
	if !openbao.Configured() {
		_, err := openbao.Read(loc.ID) // explains where it looked
		return fmt.Errorf("%s: %w", loc.ID, err)
	}
	item, err := openbao.Read(loc.ID)
	if err != nil {
		return fmt.Errorf("%s: %w", loc.ID, err)
	}
	username, password, host := item.User, item.Pass, item.Host

	if err := setProfileEnv("username", username); err != nil {
		return err
	}
	if err := setProfileEnv("password", password); err != nil {
		return err
	}
	if err := setProfileEnv("ROUTER_HOST", host); err != nil {
		return err
	}
	_ = os.Setenv("RUI_LOCATION_ID", loc.ID)
	_ = os.Setenv("RUI_LOCATION_LABEL", loc.Label)
	return nil
}

// DetectAndApplyProfile picks the location from WiFi and exports credentials.
func DetectAndApplyProfile() (Location, string, error) {
	loc, ssid, err := DetectLocation()
	if err != nil {
		return Location{}, ssid, err
	}
	if err := ApplyLocation(loc); err != nil {
		return Location{}, ssid, err
	}
	return loc, ssid, nil
}

// ApplyLocationByID loads a specific location without WiFi detection.
func ApplyLocationByID(id string) error {
	key := strings.ToUpper(strings.TrimSpace(id))
	for _, loc := range locations {
		if strings.EqualFold(loc.ID, key) {
			if !loc.Active {
				return fmt.Errorf("location %s is inactive", loc.ID)
			}
			return ApplyLocation(loc)
		}
	}
	return fmt.Errorf("unknown location %q (available: %s)", id, strings.Join(LocationNames(), ", "))
}

func (loc Location) wifiFingerprints() []string {
	out := append([]string{}, loc.WiFiSSIDs...)
	if openbao.Configured() {
		if item, err := openbao.Read(loc.ID); err == nil && item.WiFiSSID != "" {
			out = append(out, item.WiFiSSID)
		}
	}
	return out
}

func ssidMatches(got, want string) bool {
	return strings.EqualFold(strings.TrimSpace(got), strings.TrimSpace(want))
}

func locationSummary() []string {
	var out []string
	for _, loc := range locations {
		if !loc.Active {
			continue
		}
		names := loc.wifiFingerprints()
		if len(names) == 0 {
			names = []string{"?"}
		}
		out = append(out, fmt.Sprintf("%s=%s", loc.ID, strings.Join(names, "/")))
	}
	return out
}
