// jsgrep downloads Orange Livebox web UI JavaScript and extracts sysbus hints.
//
// Usage:
//
//	go run ./cmd/jsgrep
//	go run ./cmd/jsgrep -grep reserve
//	go run ./cmd/jsgrep -apps devicesList,networkAdvanced,portBinding
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/j4y-w4lk3r/rui/internal/config"
)

var (
	reServiceMethod = regexp.MustCompile(`(?i)service\s*[:=]\s*["']([^"']+)["']`)
	reMethod        = regexp.MustCompile(`(?i)method\s*[:=]\s*["']([^"']+)["']`)
	reSysbusPath    = regexp.MustCompile(`sysbus[/:]([A-Za-z0-9_.]+)[:/]([A-Za-z0-9_]+)`)
	reCallSahAPI    = regexp.MustCompile(`callSahApi(?:Idle)?\("([^"]+)"`)
	reInternalApp   = regexp.MustCompile(`internal/[a-zA-Z0-9_]+/`)
	reSah           = regexp.MustCompile(`sah=([a-zA-Z0-9._-]+)`)
)

type hit struct {
	source string
	kind   string
	value  string
}

func main() {
	envPath := flag.String("env", "", "path to .env")
	host := flag.String("host", "", "router host (default ROUTER_HOST or 192.168.1.1)")
	grep := flag.String("grep", "", "filter hits")
	apps := flag.String("apps", "devicesList,networkAdvanced,portBinding,networkTools,networkFirewall,localSettings",
		"comma-separated internal app names from loader.js")
	outDir := flag.String("out", "", "save downloaded JS here (optional)")
	catalog := flag.String("catalog", "", "write unique callSahApi refs to JSON file")
	flag.Parse()

	_ = config.BootstrapCredentials("", "")
	if p, err := config.Load(*envPath); err == nil && *host == "" {
		*host = p.Host
	}
	if *host == "" {
		*host = os.Getenv("ROUTER_HOST")
	}
	if *host == "" {
		*host = "192.168.1.1"
	}
	base := "http://" + *host

	client := &http.Client{Timeout: 20 * time.Second}

	home, err := fetch(client, base+"/")
	if err != nil {
		fmt.Fprintln(os.Stderr, "fetch /:", err)
		os.Exit(1)
	}
	sah := "a1.0.116.14_20260415"
	if m := reSah.FindStringSubmatch(home); len(m) == 2 {
		sah = m[1]
	}

	loaderURL := fmt.Sprintf("%s/loader.js?sah=%s", base, sah)
	loader, err := fetch(client, loaderURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fetch loader:", err)
		os.Exit(1)
	}

	var sources []string
	sources = append(sources, loaderURL)
	for _, app := range strings.Split(*apps, ",") {
		app = strings.TrimSpace(app)
		if app == "" {
			continue
		}
		sources = append(sources, fmt.Sprintf("%s/internal/%s/main.js?sah=%s", base, app, sah))
	}
	// Common WS helper paths seen in loader require.config
	for _, extra := range []string{
		"common/lib/ws/api",
		"sdkut/engine/api",
		"engine/api",
	} {
		sources = append(sources, fmt.Sprintf("%s/%s.js?sah=%s", base, extra, sah))
	}

	if *outDir != "" {
		_ = os.MkdirAll(*outDir, 0o755)
	}

	var all []hit
	seen := map[string]struct{}{}

	for _, u := range sources {
		body, err := fetch(client, u)
		if err != nil || len(body) < 50 {
			continue
		}
		short := u
		if i := strings.Index(u, "/internal/"); i >= 0 {
			short = u[i+1:]
		}
		if *outDir != "" {
			name := strings.NewReplacer("/", "_", "?", "_", "=", "_", "&", "_").Replace(short)
			_ = os.WriteFile(*outDir+"/"+name, []byte(body), 0o644)
		}
		for _, h := range extract(body, short) {
			key := h.kind + "\x00" + h.value
			if _, ok := seen[key]; ok {
				continue
			}
			if *grep != "" && !strings.Contains(strings.ToLower(h.value), strings.ToLower(*grep)) {
				continue
			}
			seen[key] = struct{}{}
			all = append(all, h)
		}
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].kind != all[j].kind {
			return all[i].kind < all[j].kind
		}
		return all[i].value < all[j].value
	})

	var catalogRefs []string
	if *catalog != "" {
		seenCat := map[string]struct{}{}
		for _, h := range all {
			if h.kind != "callSahApi" {
				continue
			}
			if _, ok := seenCat[h.value]; ok {
				continue
			}
			seenCat[h.value] = struct{}{}
			catalogRefs = append(catalogRefs, h.value)
		}
		sort.Strings(catalogRefs)
		raw, _ := json.MarshalIndent(catalogRefs, "", "  ")
		if err := os.WriteFile(*catalog, raw, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "catalog:", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "wrote %d refs → %s\n", len(catalogRefs), *catalog)
	}

	fmt.Printf("→ JS grep on %s (sah=%s)\n\n", base, sah)
	if len(all) == 0 {
		fmt.Println("No sysbus hints found (apps may use different bundle paths on this firmware).")
		return
	}

	cur := ""
	for _, h := range all {
		if h.kind != cur {
			cur = h.kind
			fmt.Printf("## %s\n", cur)
		}
		fmt.Printf("  [%s] %s\n", h.source, h.value)
	}

	// Also list all internal apps from loader for future targeting
	var appsFound []string
	for _, m := range reInternalApp.FindAllString(loader, -1) {
		m = strings.TrimSuffix(m, "/")
		appsFound = append(appsFound, m)
	}
	sort.Strings(appsFound)
	appsFound = uniq(appsFound)
	fmt.Printf("\n## internal apps in loader.js (%d)\n", len(appsFound))
	for _, a := range appsFound {
		if *grep == "" || strings.Contains(strings.ToLower(a), strings.ToLower(*grep)) {
			fmt.Println(" ", a)
		}
	}
}

func fetch(client *http.Client, u string) (string, error) {
	resp, err := client.Get(u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		gr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return string(raw), nil
		}
		defer gr.Close()
		dec, err := io.ReadAll(gr)
		if err != nil {
			return "", err
		}
		return string(dec), nil
	}
	return string(raw), nil
}

func extract(body, source string) []hit {
	var out []hit
	// Pair nearby service/method in minified code is hard; collect separately and sysbus paths
	for _, m := range reSysbusPath.FindAllStringSubmatch(body, -1) {
		if len(m) == 3 {
			out = append(out, hit{source, "sysbus-path", m[1] + "." + m[2]})
		}
	}
	// service/method JSON style (often in same object in source maps / unminified chunks)
	services := reServiceMethod.FindAllStringSubmatch(body, -1)
	methods := reMethod.FindAllStringSubmatch(body, -1)
	for _, s := range services {
		if len(s) == 2 && looksLikeService(s[1]) {
			out = append(out, hit{source, "service", s[1]})
		}
	}
	for _, m := range methods {
		if len(m) == 2 {
			out = append(out, hit{source, "method", m[1]})
		}
	}
	for _, m := range reCallSahAPI.FindAllStringSubmatch(body, -1) {
		if len(m) == 2 {
			out = append(out, hit{source, "callSahApi", m[1]})
		}
	}
	for _, kw := range []string{
		"setIPv4Address", "addIPv4Address", "setFixedIP", "reserveIPv4",
		"getReservedAddresses", "setReserved", "buildTopology", "setName",
		"getDevices", "getMIBs", "DHCP", "PhysAddress", "IPAddress",
	} {
		if strings.Contains(body, kw) {
			out = append(out, hit{source, "keyword", kw})
		}
	}
	return out
}

func looksLikeService(s string) bool {
	if len(s) < 2 || strings.Contains(s, " ") {
		return false
	}
	return strings.Contains(s, ".") || s == "NMC" || s == "Hosts" || s == "Devices" || s == "DeviceInfo"
}

func uniq(in []string) []string {
	if len(in) == 0 {
		return in
	}
	out := []string{in[0]}
	for _, s := range in[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}
