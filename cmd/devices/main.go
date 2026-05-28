// devices is a smoke-test CLI for the livebox.Client.ListDevices flow.
//
// It prints the same list the TUI's Devices view would render, so we can
// verify topology parsing without launching the full Bubble Tea app.
//
//	go run ./cmd/devices
//	go run ./cmd/devices --debug 2>devices.log
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/j4y-w4lk3r/rui/internal/config"
	"github.com/j4y-w4lk3r/rui/internal/livebox"
)

func main() {
	envPath := flag.String("env", ".env", "path to .env file with router credentials")
	debug := flag.Bool("debug", false, "log requests/responses to stderr")
	flag.Parse()

	cfg, err := config.Load(*envPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}

	client, err := livebox.New(cfg.Host, cfg.Username, cfg.Password)
	if err != nil {
		fmt.Fprintln(os.Stderr, "client:", err)
		os.Exit(1)
	}
	client.SetDebug(*debug)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.Login(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "login:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "logged in as %q, groups=%q\n", client.Username(), client.Groups())

	devices, err := client.ListDevices(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "list:", err)
		os.Exit(1)
	}

	fmt.Printf("Found %d devices:\n\n", len(devices))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ACTIVE\tNAME\tIP\tMAC\tTYPE\tIFACE")
	fmt.Fprintln(w, "------\t----\t--\t---\t----\t-----")
	for _, d := range devices {
		mark := "○"
		if d.Active {
			mark = "●"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			mark, d.Name, d.IPAddress, d.PhysAddress, d.DeviceType, d.Layer2Iface)
	}
	w.Flush()
}
