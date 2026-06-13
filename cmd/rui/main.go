package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/j4y-w4lk3r/rui/internal/config"
	"github.com/j4y-w4lk3r/rui/internal/livebox"
	"github.com/j4y-w4lk3r/rui/internal/tui"
)

// Stamped at link time by goreleaser via -ldflags
// "-X main.version=... -X main.commit=... -X main.date=...". Defaults
// fire on `go run` / `go build` without ldflags so we still print
// something useful in dev.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	envPath := flag.String("env", "", "path to .env file with router credentials (overrides the cascade)")
	profile := flag.String("profile", "", "load credentials from 1Password (e.g. PL → PLH2-Orange)")
	debug := flag.Bool("debug", false, "log every HTTP request/response to stderr (use with 2>debug.log)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("rui %s (commit %s, built %s)\n", version, commit, date)
		return
	}

	if *profile != "" {
		if err := config.ApplyProfile(*profile); err != nil {
			fmt.Fprintln(os.Stderr, "rui: profile error:", err)
			os.Exit(1)
		}
	}

	cfg, err := config.Load(*envPath)
	if err != nil {
		if errors.Is(err, config.ErrNoConfig) {
			printNoConfigHelp(*envPath)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "rui: config error:", err)
		os.Exit(1)
	}

	client, err := livebox.New(cfg.Host, cfg.Username, cfg.Password)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rui: client error:", err)
		os.Exit(1)
	}
	client.SetDebug(*debug)

	prog := tea.NewProgram(
		tui.New(client, cfg.Host),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "rui: fatal:", err)
		os.Exit(1)
	}
}

// printNoConfigHelp is what a fresh `brew install rui && rui` user sees
// when they haven't configured anything yet. The goal is "give them
// three obvious things to try, with copy-pasteable commands, in
// chronological order of effort".
func printNoConfigHelp(envPath string) {
	cands := config.Candidates(envPath)
	fmt.Fprintln(os.Stderr, "rui: no router credentials found.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Searched (in order):")
	for _, p := range cands {
		fmt.Fprintln(os.Stderr, "  -", p)
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "To get started, pick ONE of:")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  (a) per-user, install-anywhere — RECOMMENDED")
	suggested := suggestedConfigPath()
	fmt.Fprintln(os.Stderr, "      mkdir -p", filepath.Dir(suggested))
	fmt.Fprintln(os.Stderr, "      cat > "+suggested+" <<'EOF'")
	fmt.Fprintln(os.Stderr, "      username=admin")
	fmt.Fprintln(os.Stderr, "      password=your-router-admin-password")
	fmt.Fprintln(os.Stderr, "      # ROUTER_HOST=192.168.1.1   # optional")
	fmt.Fprintln(os.Stderr, "      EOF")
	fmt.Fprintln(os.Stderr, "      chmod 600", suggested)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  (b) project-local")
	fmt.Fprintln(os.Stderr, "      cat > .env <<'EOF'")
	fmt.Fprintln(os.Stderr, "      username=admin")
	fmt.Fprintln(os.Stderr, "      password=your-router-admin-password")
	fmt.Fprintln(os.Stderr, "      EOF")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  (c) one-shot via env vars")
	fmt.Fprintln(os.Stderr, "      username=admin password=your-router-admin-password rui")
	fmt.Fprintln(os.Stderr, "")
	if names := config.ProfileNames(); len(names) > 0 {
		fmt.Fprintln(os.Stderr, "  (d) 1Password profile (requires op CLI)")
		fmt.Fprintln(os.Stderr, "      rui -profile", names[0])
		fmt.Fprintln(os.Stderr, "")
	}
	fmt.Fprintln(os.Stderr, "Then run rui again. See `rui -h` or")
	fmt.Fprintln(os.Stderr, "https://github.com/j4y-w4lk3r/rui#first-run for details.")
}

// suggestedConfigPath returns the XDG-friendly per-user path we
// recommend, picking the same location config.Load will look in.
func suggestedConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "rui", ".env")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "rui", ".env")
	}
	return "~/.config/rui/.env"
}
