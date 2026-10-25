package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/j4y-w4lk3r/rui/internal/openbao"
)

const iotVaultID = "g3irkmq3taou5ko6gwxwlkcjd4"

// Location describes one Orange Livebox home. WiFiSSIDs (and optional WiFiItem)
// are used to auto-pick credentials when rui starts on that LAN.
type Location struct {
	ID            string
	Label         string
	Item          string
	UsernameField string
	PasswordField string
	WiFiSSIDs     []string
	WiFiItem      string // optional 1Password WiFi login; SSID read from username
	Active        bool
}

var locations = []Location{
	{
		ID:            "PLH1",
		Label:         "Warsaw",
		Item:          "plh1-orange-rui",
		UsernameField: "username",
		PasswordField: "password",
		WiFiSSIDs:     []string{"Nieszpodzianka"},
		Active:        true,
	},
	{
		ID:            "PLH2",
		Label:         "Location 3",
		Item:          "plh2-orange-rui",
		UsernameField: "username",
		PasswordField: "confirmpassword",
		WiFiSSIDs:     []string{"Orange_Swiatlowod_FC30"},
		Active:        false,
	},
	{
		ID:            "PLH3",
		Label:         "Wrocław",
		Item:          "plh3-orange-rui",
		UsernameField: "username",
		PasswordField: "confirmpassword",
		WiFiItem:      "plh3-wifi",
		Active:        true,
	},
}

// LocationNames returns active location IDs for help text.
func LocationNames() []string {
	var out []string
	for _, loc := range locations {
		if loc.Active {
			out = append(out, loc.ID)
		}
	}
	return out
}

// DetectLocation matches the current WiFi SSID to a configured location.
func DetectLocation() (Location, string, error) {
	ssid, err := currentWiFiSSID()
	if err != nil {
		return Location{}, "", err
	}
	if ssid == "" {
		return Location{}, "", fmt.Errorf("not connected to WiFi (plug in or join the home network first)")
	}

	for _, loc := range locations {
		if !loc.Active {
			continue
		}
		for _, want := range loc.wifiFingerprints() {
			if ssidMatches(ssid, want) {
				return loc, ssid, nil
			}
		}
	}
	return Location{}, ssid, fmt.Errorf("unknown location for WiFi %q (known: %s)", ssid, strings.Join(locationSummary(), "; "))
}

// ApplyLocation loads router credentials for loc from OpenBao, or from
// 1Password when the OpenBao token file is not present.
func ApplyLocation(loc Location) error {
	var username, password, host string
	if openbao.Configured() {
		item, err := openbao.Read(loc.ID)
		if err != nil {
			return fmt.Errorf("%s: %w", loc.ID, err)
		}
		username, password, host = item.User, item.Pass, item.Host
	} else {
		p := onePasswordProfile{
			VaultID:       iotVaultID,
			Item:          loc.Item,
			UsernameField: loc.UsernameField,
			PasswordField: loc.PasswordField,
		}
		var err error
		username, err = opRead(p.opRef(p.UsernameField))
		if err != nil {
			return fmt.Errorf("%s: read username: %w", loc.ID, err)
		}
		password, err = opRead(p.opRef(p.PasswordField))
		if err != nil {
			return fmt.Errorf("%s: read password: %w", loc.ID, err)
		}
		host, err = opItemHost(loc.Item)
		if err != nil {
			return fmt.Errorf("%s: read host: %w", loc.ID, err)
		}
	}

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
	} else if loc.WiFiItem != "" {
		if ssid, err := opRead(fmt.Sprintf("op://%s/%s/username", iotVaultID, loc.WiFiItem)); err == nil && ssid != "" {
			out = append(out, ssid)
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
