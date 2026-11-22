package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/j4y-w4lk3r/rui/internal/config"
	"github.com/j4y-w4lk3r/rui/internal/livebox"
	"github.com/j4y-w4lk3r/rui/internal/openbao"
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
	profile := flag.String("profile", "", "site to load from OpenBao: auto, HOME, CABIN")
	location := flag.String("location", "", "force site when not on WiFi: HOME, CABIN (same as -profile CABIN)")
	hostOverride := flag.String("host", "", "router address for Host/Origin (overrides OpenBao)")
	dialAddr := flag.String("dial", "", "TCP address to connect to instead of -host (host:port), for an SSH tunnel")
	findName := flag.String("find", "", "find a LAN device by name and print its IP (non-interactive)")
	listDevices := flag.Bool("list-devices", false, "list all LAN devices (name, IP, MAC) and exit")
	debug := flag.Bool("debug", false, "log every HTTP request/response to stderr (use with 2>debug.log)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("rui %s (commit %s, built %s)\n", version, commit, date)
		return
	}

	if err := config.BootstrapCredentials(*profile, *location); err != nil {
		fmt.Fprintln(os.Stderr, "rui:", err)
		os.Exit(1)
	}

	cfg, err := config.Load("")
	if err != nil {
		if errors.Is(err, config.ErrNoConfig) {
			printNoConfigHelp()
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "rui: config error:", err)
		os.Exit(1)
	}

	if h := strings.TrimSpace(*hostOverride); h != "" {
		cfg.Host = h
	}

	client, err := livebox.New(cfg.Host, cfg.Username, cfg.Password)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rui: client error:", err)
		os.Exit(1)
	}
	client.SetDebug(*debug)
	if err := client.SetDial(strings.TrimSpace(*dialAddr)); err != nil {
		fmt.Fprintln(os.Stderr, "rui:", err)
		os.Exit(1)
	}

	if name := strings.TrimSpace(*findName); name != "" {
		if err := runFind(client, name); err != nil {
			fmt.Fprintln(os.Stderr, "rui: find:", err)
			os.Exit(1)
		}
		return
	}

	if *listDevices {
		if err := runListDevices(client); err != nil {
			fmt.Fprintln(os.Stderr, "rui: list-devices:", err)
			os.Exit(1)
		}
		return
	}

	model := tui.New(client, cfg.Host, os.Getenv("RUI_LOCATION_LABEL"))
	model.UseDial(strings.TrimSpace(*dialAddr))
	prog := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "rui: fatal:", err)
		os.Exit(1)
	}
}

// printNoConfigHelp: no login found. rui never keeps the router password on disk
// (22/11/2026), so the help points to OpenBao/keyhole, not a .env file.
func printNoConfigHelp() {
	fmt.Fprintln(os.Stderr, "rui: no router credentials found (they are never stored on disk).")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "rui reads them on every start from:")
	fmt.Fprintln(os.Stderr, "  - OpenBao secret/rui/<site>, with the token from keyhole (entry "+openbao.KeyholeEntry+"; kh-up, stick in),")
	fmt.Fprintln(os.Stderr, "    or ~/.config/j4y/openbao-rui.token where keyhole does not run yet (macOS)")
	if names := config.ProfileNames(); len(names) > 0 {
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Pick the site if it was not detected:  rui -profile", names[0])
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "One-shot without any store:  username=admin password=... rui")
}

func runFind(client *livebox.Client, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := client.Login(ctx); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	device, err := livebox.FindByName(ctx, client, name)
	if err != nil {
		return err
	}
	if device.IPAddress == "" {
		return fmt.Errorf("device %q has no IP address", name)
	}
	fmt.Println(device.IPAddress)
	return nil
}

func runListDevices(client *livebox.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.Login(ctx); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	devices, err := client.ListDevices(ctx)
	if err != nil {
		return err
	}

	fmt.Println("NAME\tIP\tMAC\tACTIVE")
	for _, d := range devices {
		active := "no"
		if d.Active {
			active = "yes"
		}
		fmt.Printf("%s\t%s\t%s\t%s\n",
			strings.TrimSpace(d.Name),
			strings.TrimSpace(d.IPAddress),
			strings.TrimSpace(d.PhysAddress),
			active,
		)
	}
	fmt.Printf("--- %d device(s) ---\n", len(devices))
	return nil
}
