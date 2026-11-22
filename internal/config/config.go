package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds router connection info loaded from the environment.
type Config struct {
	Host     string
	Username string
	Password string
}

// ErrNoConfig is returned by Load when no router credentials are in the
// process environment (rui puts them there from OpenBao).
var ErrNoConfig = errors.New("no router credentials (OpenBao)")

// Load reads the router login from the process environment (username,
// password, ROUTER_HOST), where BootstrapCredentials put it from
// OpenBao. Nothing is read from or written to disk for rui itself
// (22/11/2026: the old ~/.config/rui/.env cascade is gone).
//
// envPath is only for the developer tools under cmd/ (probe, lab, …): when set
// and the file exists, it is read first; rui passes "".
func Load(envPath string) (*Config, error) {
	found := ""
	if envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			if err := godotenv.Load(envPath); err != nil {
				return nil, fmt.Errorf("read %s: %w", envPath, err)
			}
			found = envPath
		}
	}

	cfg := &Config{
		Host:     strings.TrimSpace(getEnv("ROUTER_HOST", "192.168.1.1")),
		Username: strings.TrimSpace(os.Getenv("username")),
		Password: os.Getenv("password"),
	}
	if cfg.Username == "" && cfg.Password == "" {
		return nil, ErrNoConfig
	}
	source := found
	if source == "" {
		source = "OpenBao"
	}
	if cfg.Username == "" {
		return nil, fmt.Errorf("missing username from %s", source)
	}
	if cfg.Password == "" {
		return nil, fmt.Errorf("missing password from %s", source)
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
