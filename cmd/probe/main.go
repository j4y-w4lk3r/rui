// probe is a tiny CLI for poking individual sysbus services while we figure
// out which calls actually work on this Funbox firmware.
//
// Usage:
//
//	go run ./cmd/probe -- <service> <method> ['{"json":"params"}']
//
// Examples:
//
//	go run ./cmd/probe -- Devices get
//	go run ./cmd/probe -- Hosts getDevices
//	go run ./cmd/probe -- NMC getWANStatus
//	go run ./cmd/probe -- TopologyDiagnostics buildTopology '{"SendXmlFile":false}'
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/j4y-w4lk3r/rui/internal/config"
	"github.com/j4y-w4lk3r/rui/internal/livebox"
)

func main() {
	envPath := flag.String("env", ".env", "path to .env file with router credentials")
	debug := flag.Bool("debug", true, "log requests/responses to stderr")
	flag.Parse()

	args := flag.Args()
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: probe <service> <method> [json-params]")
		os.Exit(2)
	}
	service, method := args[0], args[1]
	var params map[string]any
	if len(args) >= 3 {
		if err := json.Unmarshal([]byte(args[2]), &params); err != nil {
			fmt.Fprintln(os.Stderr, "invalid JSON params:", err)
			os.Exit(2)
		}
	}

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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Login(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "login:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "logged in as %q, groups=%q\n", client.Username(), client.Groups())

	raw, err := client.RawCall(ctx, service, method, params)
	if err != nil {
		fmt.Fprintln(os.Stderr, "call:", err)
		os.Exit(1)
	}

	var pretty any
	if err := json.Unmarshal(raw, &pretty); err == nil {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(pretty)
	} else {
		os.Stdout.Write(raw)
		fmt.Println()
	}
}
