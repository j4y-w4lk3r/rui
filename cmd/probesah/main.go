// probesah reads callSahApi names from jsgrep-style catalog and tries each
// via POST /sysbus/{service}:{method} and POST /ws — read-only calls only by default.
//
// Usage:
//
//	go run ./cmd/probesah
//	go run ./cmd/probesah -grep Dhcp
//	go run ./cmd/probesah -write   # include mutating calls (careful)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/j4y-w4lk3r/rui/internal/config"
	"github.com/j4y-w4lk3r/rui/internal/livebox"
)

var readOnlyPrefixes = []string{
	"get", "list", "retrieve", "build", "is", "fastList",
}

var writePrefixes = []string{
	"add", "set", "delete", "create", "update", "configure", "manage", "activate", "enregistrer",
}

func main() {
	envPath := flag.String("env", "", "path to .env")
	grep := flag.String("grep", "", "filter API refs")
	write := flag.Bool("write", false, "also probe mutating methods (default: read-only)")
	catalog := flag.String("catalog", "", "JSON file of refs (default: run built-in list from jsgrep output)")
	timeout := flag.Duration("timeout", 8*time.Second, "per-call timeout")
	flag.Parse()

	_ = config.BootstrapCredentials("", "")
	cfg, err := config.Load(*envPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}

	refs := loadRefs(*catalog, *grep)
	if len(refs) == 0 {
		fmt.Fprintln(os.Stderr, "no API refs to probe")
		os.Exit(1)
	}

	client, err := livebox.New(cfg.Host, cfg.Username, cfg.Password)
	if err != nil {
		fmt.Fprintln(os.Stderr, "client:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(len(refs)+1)* *timeout)
	defer cancel()

	if err := client.Login(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "login:", err)
		os.Exit(1)
	}
	defer func() { _ = client.Logout(context.Background()) }()

	fmt.Printf("→ %s ProductClass probe via /sysbus + /ws (%d refs, write=%v)\n\n", cfg.Host, len(refs), *write)

	var ok, fail, skip int
	for _, ref := range refs {
		if !*write && isWrite(ref) {
			skip++
			continue
		}
		svc, meth, okParse := livebox.ParseSahRef(ref)
		if !okParse {
			fail++
			fmt.Printf("✗ %-55s PARSE\n", ref)
			continue
		}

		callCtx, cancel := context.WithTimeout(ctx, *timeout)
		raw, via, err := tryBoth(callCtx, client, svc, meth)
		cancel()

		if err != nil {
			fail++
			fmt.Printf("✗ %-55s %s — %v\n", ref, via, shortErr(raw, err))
			continue
		}
		if apiErr := apiErrorMsg(raw); apiErr != "" {
			fail++
			fmt.Printf("✗ %-55s %s — %s\n", ref, via, apiErr)
			continue
		}
		ok++
		fmt.Printf("✓ %-55s %s\n", ref, via)
	}

	fmt.Printf("\n═══ %d OK / %d fail / %d skipped (write methods)\n", ok, fail, skip)
}

func tryBoth(ctx context.Context, c *livebox.Client, svc, meth string) (raw json.RawMessage, via string, err error) {
	raw, err = c.SysbusPost(ctx, svc, meth, map[string]any{})
	if err == nil && apiErrorMsg(raw) == "" {
		return raw, "sysbus", nil
	}
	sysbusErr := err
	sysbusAPI := apiErrorMsg(raw)

	raw, err = c.RawCall(ctx, svc, meth, map[string]any{})
	if err == nil && apiErrorMsg(raw) == "" {
		return raw, "ws", nil
	}
	if sysbusErr != nil && err != nil {
		return raw, "both", fmt.Errorf("sysbus: %v; ws: %v", sysbusErr, err)
	}
	if sysbusAPI != "" {
		return raw, "both", fmt.Errorf("sysbus: %s; ws: %s", sysbusAPI, apiErrorMsg(raw))
	}
	return raw, "ws", err
}

func apiErrorMsg(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "empty response"
	}
	var env struct {
		Errors []struct {
			Description string `json:"description"`
			Info        string `json:"info"`
		} `json:"errors"`
		Status any `json:"status"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return ""
	}
	if len(env.Errors) == 0 {
		return ""
	}
	e := env.Errors[0]
	if e.Info != "" {
		return e.Description + " — " + e.Info
	}
	return e.Description
}

func shortErr(raw json.RawMessage, err error) string {
	if msg := apiErrorMsg(raw); msg != "" {
		return msg
	}
	return err.Error()
}

func isWrite(ref string) bool {
	_, meth, ok := livebox.ParseSahRef(ref)
	if !ok {
		return true
	}
	low := strings.ToLower(meth)
	for _, p := range writePrefixes {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	for _, p := range readOnlyPrefixes {
		if strings.HasPrefix(low, p) {
			return false
		}
	}
	return true // unknown → treat as write
}

func loadRefs(catalogPath, grep string) []string {
	var refs []string
	if catalogPath != "" {
		raw, err := os.ReadFile(catalogPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "catalog:", err)
			os.Exit(1)
		}
		_ = json.Unmarshal(raw, &refs)
	} else {
		// Default: extract via jsgrep logic — run subprocess or embed common refs.
		// For now read from jsgrep -catalog if exists, else builtin DHCP/device set.
		refs = defaultRefs()
	}
	grep = strings.ToLower(grep)
	var out []string
	seen := map[string]struct{}{}
	for _, r := range refs {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if grep != "" && !strings.Contains(strings.ToLower(r), grep) {
			continue
		}
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

func defaultRefs() []string {
	// Full set extracted from jsgrep on Funbox 10 firmware a1.0.116.14_20260415
	return []string{
		"sah.Connection.Client.Domino.getStatus",
		"sah.Connection.Client.getCurrentConfiguration",
		"sah.Connection.Client.Ipsec.getStatus",
		"sah.Connection.Client.Routes.getStaticRoutes",
		"sah.Connection.Client.Wan.getStatus",
		"sah.Connection.Dns.getState",
		"sah.Connection.Server.WiFi.listAccessPoints",
		"sah.Device.Information.getLanguage",
		"sah.Device.Network.getIPv6Status",
		"sah.Device.SmartLife.getStatus",
		"sah.Device.Storage.listStorages",
		"sah.Device.Time.getTime",
		"sah.Device.User.listUsers",
		"sah.HomeNetwork.DhcpServerIpv4.getConfiguration",
		"sah.HomeNetwork.DhcpServerIpv4.getIpv4Configuration",
		"sah.HomeNetwork.DhcpServerIpv4.listStaticLeases",
		"sah.HomeNetwork.DhcpServerIpv6.getIpv6Configuration",
		"sah.HomeNetwork.DynDns.getHostname",
		"sah.HomeNetwork.DynDns.getOrangeDynDnsState",
		"sah.HomeNetwork.DynDns.listServices",
		"sah.HomeNetwork.DynDns.listUserAccount",
		"sah.HomeNetwork.Firewall.DmzIpv4.getCurrentState",
		"sah.HomeNetwork.Firewall.NatPatIpv4.listRules",
		"sah.HomeNetwork.Firewall.PCP.get",
		"sah.HomeNetwork.Firewall.ProtocolForwarding.get",
		"sah.HomeNetwork.NetworkMap.fastListDevices",
		"sah.HomeNetwork.NetworkMap.getDeviceMap",
		"sah.HomeNetwork.NetworkMap.listDevices",
		"sah.HomeNetwork.UpnpServer.getCurrentState",
	}
}
