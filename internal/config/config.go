package config

import (
	"errors"
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

// ErrNoConfig is returned by Load when no .env file was found in any of
// the search locations AND no router credentials were exported in the
// process environment. main() catches this and prints a friendly setup
// walkthrough instead of a terse one-line error, which is the difference
// between "I bounced off this tool" and "I know what to do next" for a
// fresh `brew install` user.
var ErrNoConfig = errors.New("no .env file found and no router credentials in environment")

// Load reads credentials by walking a cascade of likely locations and
// then falling back to the process env. Lookup order:
//
//  1. The path passed in via -env <path>, IF it was explicitly set
//     (treated as authoritative — if you point at a file we don't
//     auto-fall-back to other locations on miss).
//  2. $PWD/.env             (project-local; matches README setup)
//  3. $XDG_CONFIG_HOME/rui/.env or ~/.config/rui/.env  (per-user, the
//     standard "I installed this with brew and run it from anywhere" home)
//  4. .env next to the binary on disk (source-tree dev builds)
//  5. Process env vars (`username=… password=… rui`)
//
// A missing file is fine — we move down the chain. A malformed file
// (parse error) is fatal.
func Load(envPath string) (*Config, error) {
	candidates := envCandidates(envPath)
	found := ""
	for _, p := range candidates {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := godotenv.Load(p); err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		found = p
		break
	}

	cfg := &Config{
		Host:     strings.TrimSpace(getEnv("ROUTER_HOST", "192.168.1.1")),
		Username: strings.TrimSpace(os.Getenv("username")),
		Password: os.Getenv("password"),
	}

	// Both empty AND no file loaded → user has not configured anything.
	// This is the case for a fresh `brew install rui && rui` and deserves
	// a friendly walkthrough rather than a stack trace.
	if cfg.Username == "" && cfg.Password == "" && found == "" {
		return nil, ErrNoConfig
	}

	source := found
	if source == "" {
		source = "process environment"
	}
	if cfg.Username == "" {
		return nil, fmt.Errorf("missing 'username' in %s (searched: %v)", source, candidates)
	}
	if cfg.Password == "" {
		return nil, fmt.Errorf("missing 'password' in %s (searched: %v)", source, candidates)
	}
	return cfg, nil
}

// envCandidates returns the ordered list of paths Load will try.
// Exposed via Candidates() so the friendly help can show the user
// exactly which locations were searched.
func envCandidates(envPath string) []string {
	// Explicit -env <path> short-circuits the cascade.
	if envPath != "" && envPath != ".env" {
		return []string{envPath}
	}

	var out []string

	// 1. $PWD/.env — project-local; the README's "drop creds into .env"
	//    flow.
	if cwd, err := os.Getwd(); err == nil {
		out = append(out, filepath.Join(cwd, ".env"))
	}

	// 2. $XDG_CONFIG_HOME/rui/.env or ~/.config/rui/.env — the user-wide
	//    install-anywhere home. Lets you `brew install rui && rui` from
	//    any directory once your creds are stashed there.
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		out = append(out, filepath.Join(xdg, "rui", ".env"))
	} else if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, ".config", "rui", ".env"))
	}

	// 3. .env next to the binary — handy in source-tree dev (`go run .`
	//    leaves the binary in $PWD, but `./build/rui` finds the .env that
	//    ships with the project tree).
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		out = append(out, filepath.Join(filepath.Dir(exe), ".env"))
	}
	return out
}

// Candidates returns the ordered list of locations Load will search,
// for use in user-facing help text.
func Candidates(envPath string) []string {
	return envCandidates(envPath)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
