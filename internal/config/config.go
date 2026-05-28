package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds router connection info loaded from the environment.
type Config struct {
	Host     string
	Username string
	Password string
}

// Load reads credentials from a .env file (and from the process env as
// a fallback). The lookup order is:
//
//  1. The path passed in by `-env <path>`, IF the user actually set it
//     (not the default ".env").
//  2. The default ".env" in the current working directory.
//  3. ".env" next to the executable — so launching `./orange-tui` from
//     any directory still finds the credentials that ship with the
//     binary's project tree.
//
// We don't treat a missing file as fatal: a user who exports
// `username`/`password` directly should still be able to run the TUI.
func Load(envPath string) (*Config, error) {
	for _, p := range envCandidates(envPath) {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := godotenv.Load(p); err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		envPath = p // report the file we actually loaded in error messages
		break
	}

	cfg := &Config{
		Host:     strings.TrimSpace(getEnv("ROUTER_HOST", "192.168.1.1")),
		Username: strings.TrimSpace(os.Getenv("username")),
		Password: os.Getenv("password"),
	}

	if cfg.Username == "" {
		return nil, fmt.Errorf("missing 'username' in %s (tried %v)", envPath, envCandidates(envPath))
	}
	if cfg.Password == "" {
		return nil, fmt.Errorf("missing 'password' in %s (tried %v)", envPath, envCandidates(envPath))
	}

	return cfg, nil
}

// envCandidates returns the ordered list of paths Load will try. Kept
// exported via the error message so users can immediately see which
// locations were searched.
func envCandidates(envPath string) []string {
	var out []string
	if envPath != "" {
		out = append(out, envPath)
	}
	// .env next to the binary on disk. Resolves symlinks so a binary
	// installed to ~/bin/rui via a symlink still finds the real
	// project's .env.
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		out = append(out, filepath.Join(filepath.Dir(exe), ".env"))
	}
	return out
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
