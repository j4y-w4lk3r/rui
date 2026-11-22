package config

import (
	"fmt"
	"os"
	"strings"
)

// ApplyProfile reads router credentials for name from OpenBao and exports them into the
// process environment (username, password, ROUTER_HOST), in memory only. name is "auto"
// (detect the site) or a site ID from settings.json. 1Password is not used.
func ApplyProfile(name string) error {
	key := strings.ToUpper(strings.TrimSpace(name))
	if key == "AUTO" {
		_, _, err := DetectAndApplyProfile()
		return err
	}
	for _, loc := range locations() {
		if strings.EqualFold(loc.ID, key) {
			return ApplyLocationByID(key)
		}
	}
	return fmt.Errorf("unknown profile %q (available: %s)", name, strings.Join(ProfileNames(), ", "))
}

// ProfileNames returns the known profile keys for help text.
func ProfileNames() []string {
	return append([]string{"auto"}, LocationNames()...)
}

func setProfileEnv(key, value string) error {
	if value == "" {
		return fmt.Errorf("empty value for %s", key)
	}
	return os.Setenv(key, value)
}
