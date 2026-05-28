package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/j4y-w4lk3r/rui/internal/config"
	"github.com/j4y-w4lk3r/rui/internal/livebox"
	"github.com/j4y-w4lk3r/rui/internal/tui"
)

// Stamped at link time by goreleaser via -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
// Defaults fire on `go run` / `go build` without ldflags so we still print something useful in dev.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	envPath := flag.String("env", ".env", "path to .env file with router credentials")
	debug := flag.Bool("debug", false, "log every HTTP request/response to stderr (use with 2>debug.log)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("rui %s (commit %s, built %s)\n", version, commit, date)
		return
	}

	cfg, err := config.Load(*envPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}

	client, err := livebox.New(cfg.Host, cfg.Username, cfg.Password)
	if err != nil {
		fmt.Fprintln(os.Stderr, "client error:", err)
		os.Exit(1)
	}
	client.SetDebug(*debug)

	prog := tea.NewProgram(
		tui.New(client, cfg.Host),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}
