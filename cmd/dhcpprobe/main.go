// dhcpprobe exhaustively tries DHCP-related APIs on Funbox 10 with every
// known transport variant, logs results, and optionally attempts reservation.
//
// Usage:
//
//	go run ./cmd/dhcpprobe
//	go run ./cmd/dhcpprobe -reserve   # try addStaticLease for NAS .65
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/j4y-w4lk3r/rui/internal/config"
	"github.com/j4y-w4lk3r/rui/internal/livebox"
)

const (
	nasMAC = "D0:EA:11:BB:C8:59"
	nasIP  = "192.168.1.65"
)

var dhcpRefs = []string{
	"sah.HomeNetwork.DhcpServerIpv4.listStaticLeases",
	"sah.HomeNetwork.DhcpServerIpv4.getConfiguration",
	"sah.HomeNetwork.DhcpServerIpv4.getIpv4Configuration",
	"sah.HomeNetwork.DhcpServerIpv4.addStaticLease",
	"sah.HomeNetwork.DhcpServerIpv4.deleteStaticLease",
}

var legacyProbes = []struct {
	service, method string
	params map[string]any
}{
	{"Devices.Device." + nasMAC, "get", nil},
	{"Hosts", "getReservedAddresses", nil},
	{"HostManager", "getDevices", nil},
	{"DHCPv4.Server", "get", nil},
	{"DHCPv4.Server.Pool", "get", nil},
	{"DHCPv4.Server.Pool.default", "getStaticLeases", nil},
	{"NMC", "getWANStatus", nil},
}

var legacyReserve = struct {
	service, method string
	params map[string]any
}{
	"DHCPv4.Server.Pool.default", "addStaticLease",
	map[string]any{"MACAddress": nasMAC, "IPAddress": nasIP},
}

func main() {
	reserve := flag.Bool("reserve", false, "attempt addStaticLease for NAS (mutating)")
	outDir := flag.String("out", "reports", "write probe log here")
	flag.Parse()

	_ = config.BootstrapCredentials("", "")
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}

	client, err := livebox.New(cfg.Host, cfg.Username, cfg.Password)
	if err != nil {
		fmt.Fprintln(os.Stderr, "client:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	if err := client.Login(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "login:", err)
		os.Exit(1)
	}
	defer func() { _ = client.Logout(context.Background()) }()

	ts := time.Now().Format("2006-01-02_15-04-05")
	logPath := filepath.Join(*outDir, "dhcpprobe-"+ts+".md")
	var log strings.Builder

	fmt.Fprintf(&log, "# DHCP probe — %s\n\n", ts)
	fmt.Fprintf(&log, "- Host: `%s`\n", cfg.Host)
	fmt.Fprintf(&log, "- Profile: PLH3 (Funbox 10)\n\n")

	fmt.Println("→ DHCP exhaustive probe on", cfg.Host)

	for _, ref := range dhcpRefs {
		if strings.Contains(ref, "addStatic") || strings.Contains(ref, "deleteStatic") {
			if !*reserve {
				fmt.Printf("⊘ skip %s (use -reserve)\n", ref)
				continue
			}
		}
		params := map[string]any{}
		if strings.Contains(ref, "addStaticLease") {
			params = map[string]any{"deviceId": nasMAC, "ipAddress": nasIP}
		}
		if strings.Contains(ref, "deleteStaticLease") {
			params = map[string]any{"deviceId": nasMAC}
		}

		fmt.Printf("\n## %s\n", ref)
		log.WriteString("\n## " + ref + "\n\n")
		results := client.ProbeSah(ctx, ref, params)
		anyOK := false
		for _, r := range results {
			apiErr := livebox.APIErrorDescription(r.Body)
			status := "FAIL"
			if r.Err == nil && r.StatusCode == 200 && apiErr == "" {
				status = "OK"
				anyOK = true
			}
			line := fmt.Sprintf("- **%s** `[%s]` HTTP %d", status, r.Variant.Name, r.StatusCode)
			if apiErr != "" {
				line += " — " + apiErr
			}
			if r.Err != nil {
				line += " — " + r.Err.Error()
			}
			fmt.Println(line)
			log.WriteString(line + "\n")
			if status == "OK" || apiErr != "" {
				pretty, _ := json.MarshalIndent(json.RawMessage(r.Body), "", "  ")
				if len(pretty) > 1200 {
					pretty = append(pretty[:1200], []byte("\n  …")...)
				}
				log.WriteString("\n```json\n" + string(pretty) + "\n```\n")
			}
		}
		if anyOK {
			fmt.Println("✓ at least one variant worked")
		}
	}

	if *reserve {
		fmt.Printf("\n## Legacy reserve (Pool.default.addStaticLease)\n")
		log.WriteString("\n## Legacy reserve (Pool.default.addStaticLease)\n\n")
		results := client.ProbeCall(ctx, legacyReserve.service, legacyReserve.method, legacyReserve.params)
		if r, ok := livebox.BestResult(results); ok {
			line := fmt.Sprintf("- **OK** `%s.%s` via %s", legacyReserve.service, legacyReserve.method, r.Variant.Name)
			fmt.Println(line)
			log.WriteString(line + "\n")
			pretty, _ := json.MarshalIndent(json.RawMessage(r.Body), "", "  ")
			log.WriteString("\n```json\n" + string(pretty) + "\n```\n")
		} else {
			raw, err := client.RawCall(ctx, legacyReserve.service, legacyReserve.method, legacyReserve.params)
			apiErr := livebox.APIErrorDescription(raw)
			line := fmt.Sprintf("- **FAIL** `%s.%s` — %s", legacyReserve.service, legacyReserve.method, apiErr)
			if err != nil {
				line += " " + err.Error()
			}
			fmt.Println(line)
			log.WriteString(line + "\n")
		}
		leases, err := client.ListStaticDHCPLeases(ctx)
		if err == nil {
			fmt.Printf("- verify getStaticLeases: %d lease(s)\n", len(leases))
			for _, l := range leases {
				fmt.Printf("  • %s → %s\n", l.MACAddress, l.IPAddress)
			}
		}
	}

	fmt.Println("\n## Legacy sysbus probes")
	log.WriteString("\n## Legacy sysbus probes\n\n")
	for _, p := range legacyProbes {
		label := p.service + "." + p.method
		results := client.ProbeCall(ctx, p.service, p.method, p.params)
		if r, ok := livebox.BestResult(results); ok {
			line := fmt.Sprintf("- **OK** `%s` via %s", label, r.Variant.Name)
			fmt.Println(line)
			log.WriteString(line + "\n")
			continue
		}
		raw, err := client.RawCall(ctx, p.service, p.method, p.params)
		apiErr := livebox.APIErrorDescription(raw)
		status := "OK"
		if err != nil || apiErr != "" {
			status = "FAIL"
		}
		line := fmt.Sprintf("- **%s** `%s` — %s", status, label, apiErr)
		if err != nil {
			line += " " + err.Error()
		}
		fmt.Println(line)
		log.WriteString(line + "\n")
	}

	if err := os.MkdirAll(*outDir, 0o755); err == nil {
		_ = os.WriteFile(logPath, []byte(log.String()), 0o644)
		fmt.Printf("\n✓ Wrote %s\n", logPath)
	}
}
