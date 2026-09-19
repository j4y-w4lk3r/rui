// introspect queries the Orange Livebox datamodel (GET /sysbus/...) without a browser.
//
// Usage:
//
//	go run ./cmd/introspect
//	go run ./cmd/introspect -path Hosts -grep dhcp
//	go run ./cmd/introspect -path Devices -depth 2
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/j4y-w4lk3r/rui/internal/config"
	"github.com/j4y-w4lk3r/rui/internal/livebox"
)

var defaultPaths = []string{
	"Hosts", "HostManager", "Devices", "NeMo.Intf.lan",
	"NMC", "NMC.Wifi", "NMC.Guest", "DHCP", "DHCPServer",
	"IP", "LAN", "NeMo", "TopologyDiagnostics",
}

func main() {
	envPath := flag.String("env", "", "path to .env")
	path := flag.String("path", "", "datamodel path (empty = scan defaults)")
	depth := flag.Int("depth", -1, "REST depth (-1 = full tree)")
	grep := flag.String("grep", "", "filter methods by substring")
	debug := flag.Bool("debug", false, "log HTTP to stderr")
	flag.Parse()

	_ = config.BootstrapCredentials("", "")
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

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	if err := client.Login(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "login:", err)
		os.Exit(1)
	}
	defer func() { _ = client.Logout(context.Background()) }()

	paths := defaultPaths
	if *path != "" {
		paths = strings.Split(*path, ",")
	}

	fmt.Printf("→ %s (datamodel introspection, depth=%d)\n\n", cfg.Host, *depth)

	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		nodes, err := client.GetModel(ctx, p, *depth)
		if err != nil {
			fmt.Printf("## %s — ERROR: %v\n\n", p, err)
			continue
		}

		found := livebox.FindFunctions(nodes, *grep)
		fmt.Printf("## %s — %d method(s)\n", p, len(found))
		for _, f := range found {
			paramNames := make([]string, 0, len(f.Params))
			for _, par := range f.Params {
				s := par.Name
				if par.Type != "" {
					s += ":" + par.Type
				}
				paramNames = append(paramNames, s)
			}
			svc := f.Path
			if svc == "" {
				svc = p
			}
			fmt.Printf("  %s.%s(%s)\n", svc, f.Method, strings.Join(paramNames, ", "))
		}
		fmt.Println()
	}
}
