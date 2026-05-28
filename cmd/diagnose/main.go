// diagnose probes a battery of sysbus endpoints against the box and writes a
// human-readable report. Use it to figure out which calls succeed and which
// are denied for the current session, without having to rebuild the TUI.
//
// Usage:
//
//	go run ./cmd/diagnose
//	go run ./cmd/diagnose -out diagnose-2026-05-18.md
//	go run ./cmd/diagnose -only NMC,Devices,Topology   # filter by tag
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/j4y-w4lk3r/rui/internal/config"
	"github.com/j4y-w4lk3r/rui/internal/livebox"
)

// probe describes a single endpoint to test. tags lets the user filter with
// -only / -skip; expect is a short note about what we hope to see.
type probe struct {
	name    string         // short label used in the report
	tags    []string       // arbitrary categorization, e.g. "NMC", "Devices"
	service string         // sysbus service
	method  string         // sysbus method
	params  map[string]any // request parameters
	expect  string         // human-readable description of what success looks like
}

// catalog of endpoints to test. These are sourced from the real Funbox 7 web
// UI capture; if you find new ones, add them here.
var catalog = []probe{
	// ----- basics -----
	{name: "DeviceInfo.get", tags: []string{"basics"}, service: "DeviceInfo", method: "get",
		expect: "model, firmware version, uptime, serial"},
	{name: "Time.getTime", tags: []string{"basics"}, service: "Time", method: "getTime",
		expect: "current time on the box"},
	{name: "HTTPService.getCurrentUser", tags: []string{"basics", "auth"}, service: "HTTPService", method: "getCurrentUser",
		expect: "the user the session is logged in as"},
	{name: "UserManagement.getUsers", tags: []string{"auth"}, service: "UserManagement", method: "getUsers",
		expect: "full list of configured users (admin only)"},
	{name: "UserManagement.getUser(admin)", tags: []string{"auth"}, service: "UserManagement", method: "getUser",
		params: map[string]any{"name": "admin"}, expect: "the admin user record (with role/groups)"},
	{name: "UserInterface.getState", tags: []string{"basics"}, service: "UserInterface", method: "getState"},
	{name: "UserInterface.getLanguage", tags: []string{"basics"}, service: "UserInterface", method: "getLanguage"},
	{name: "Manifests.retrieve", tags: []string{"basics"}, service: "Manifests", method: "retrieve",
		params: map[string]any{"user": "admin", "option": "wm_settings"},
		expect: "GUI configuration manifest (admin-only on some firmwares)"},

	// ----- WAN / Internet -----
	{name: "NMC.getWANStatus", tags: []string{"NMC", "wan"}, service: "NMC", method: "getWANStatus",
		expect: "public IPv4, gateway, DNS, MAC"},
	{name: "NMC.get", tags: []string{"NMC"}, service: "NMC", method: "get",
		expect: "WAN settings"},
	{name: "NMC.OrangeTV.getIPTVStatus", tags: []string{"NMC", "tv"}, service: "NMC.OrangeTV", method: "getIPTVStatus"},
	{name: "NMC.Guest.get", tags: []string{"NMC"}, service: "NMC.Guest", method: "get"},

	// ----- Wi-Fi -----
	{name: "NMC.Wifi.get", tags: []string{"wifi"}, service: "NMC.Wifi", method: "get",
		expect: "global Wi-Fi enabled / status"},
	{name: "NeMo.Intf.lan.getMIBs(wlanvap)", tags: []string{"wifi"}, service: "NeMo.Intf.lan", method: "getMIBs",
		params: map[string]any{"mibs": "wlanvap base", "flag": "wlanvap"},
		expect: "per-SSID details (channel, encryption, etc.)"},
	{name: "NeMo.Intf.lan.getMIBs(radio)", tags: []string{"wifi"}, service: "NeMo.Intf.lan", method: "getMIBs",
		params: map[string]any{"mibs": "base wlanradio"},
		expect: "radio config (2.4 / 5 GHz)"},

	// ----- Devices / Topology -----
	{name: "TopologyDiagnostics.buildTopology", tags: []string{"devices", "topology"}, service: "TopologyDiagnostics", method: "buildTopology",
		params: map[string]any{"SendXmlFile": false},
		expect: "tree of all known devices — this is what the web UI uses"},
	{name: "Devices.get(bare)", tags: []string{"devices"}, service: "Devices", method: "get",
		expect: "flat list of devices"},
	{name: "Devices.get(filtered)", tags: []string{"devices"}, service: "Devices", method: "get",
		params: map[string]any{"expression": map[string]any{
			"ETHERNET": "not interface and not self and eth and .Active==true",
			"WIFI":     "not interface and not self and wifi and .Active==true",
		}}},
	{name: "Hosts.getDevices", tags: []string{"devices"}, service: "Hosts", method: "getDevices"},
	{name: "HostManager.getDevices", tags: []string{"devices"}, service: "HostManager", method: "getDevices"},

	// ----- Voice -----
	{name: "VoiceService.listTrunks", tags: []string{"voice"}, service: "VoiceService.VoiceApplication", method: "listTrunks"},
	{name: "VoiceService.getCallList", tags: []string{"voice"}, service: "VoiceService.VoiceApplication", method: "getCallList",
		params: map[string]any{"line": "1"}},
	{name: "Phonebook.getAllContacts", tags: []string{"voice"}, service: "Phonebook", method: "getAllContacts"},

	// ----- Power / Scheduler / IoT -----
	{name: "PowerManagement.get", tags: []string{"power"}, service: "PowerManagement", method: "get"},
	{name: "PowerManagement.getProfiles", tags: []string{"power"}, service: "PowerManagement", method: "getProfiles",
		params: map[string]any{"profiles": []string{"Light", "Deep", "WiFi", "LED"}}},
	{name: "Scheduler.getCompleteSchedules(WLAN)", tags: []string{"power"}, service: "Scheduler", method: "getCompleteSchedules",
		params: map[string]any{"type": "WLAN"}},
	{name: "IoTService.getStatus", tags: []string{"iot"}, service: "IoTService", method: "getStatus"},
}

type result struct {
	probe    probe
	ok       bool
	status   string  // OK / ERROR
	errMsg   string  // populated when !ok
	body     string  // pretty-printed response (truncated)
	bodyFull string  // untruncated, for the markdown report
	took     time.Duration
}

func main() {
	envPath := flag.String("env", ".env", "path to .env file")
	outPath := flag.String("out", "", "path to write markdown report (default: reports/diagnose-<timestamp>.md)")
	onlyStr := flag.String("only", "", "comma-separated tags to include (empty = all)")
	skipStr := flag.String("skip", "", "comma-separated tags to exclude")
	timeout := flag.Duration("timeout", 6*time.Second, "per-call timeout")
	maxBody := flag.Int("max-body", 1200, "max response bytes shown in the on-screen summary")
	quiet := flag.Bool("quiet", false, "don't print live results (just write the report)")
	debug := flag.Bool("debug", false, "log every HTTP request/response to stderr")
	flag.Parse()

	if *outPath == "" {
		*outPath = filepath.Join("reports", "diagnose-"+time.Now().Format("2006-01-02_15-04-05")+".md")
	}
	if dir := filepath.Dir(*outPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fail("create output dir: %v", err)
		}
	}

	cfg, err := config.Load(*envPath)
	if err != nil {
		fail("config: %v", err)
	}

	only := splitTags(*onlyStr)
	skip := splitTags(*skipStr)

	client, err := livebox.New(cfg.Host, cfg.Username, cfg.Password)
	if err != nil {
		fail("client: %v", err)
	}
	client.SetDebug(*debug)

	fmt.Printf("→ Target: %s as %s\n", cfg.Host, cfg.Username)
	fmt.Printf("→ Login... ")
	loginStart := time.Now()
	loginCtx, cancel := context.WithTimeout(context.Background(), *timeout)
	if err := client.Login(loginCtx); err != nil {
		cancel()
		fail("login failed: %v", err)
	}
	cancel()
	fmt.Printf("OK (%s) groups=%q\n\n", time.Since(loginStart).Truncate(time.Millisecond), client.Groups())

	// Run all the probes serially. Concurrency would speed this up but
	// sequential output is much easier to read while debugging.
	var results []result
	for _, p := range catalog {
		if !matchTags(p.tags, only, skip) {
			continue
		}
		r := runProbe(client, p, *timeout, *maxBody)
		results = append(results, r)
		if !*quiet {
			printLive(r, *maxBody)
		}
	}

	// On-screen summary.
	okCount := 0
	for _, r := range results {
		if r.ok {
			okCount++
		}
	}
	fmt.Printf("\n══════════════════════════════════════════════════\n")
	fmt.Printf("Summary: %d/%d OK\n", okCount, len(results))
	fmt.Printf("══════════════════════════════════════════════════\n")
	for _, r := range results {
		icon := "✗"
		if r.ok {
			icon = "✓"
		}
		line := fmt.Sprintf("%s %-40s %s", icon, r.probe.name, r.status)
		if !r.ok {
			line += " — " + truncate(r.errMsg, 70)
		}
		fmt.Println(line)
	}

	// Markdown report.
	if err := writeReport(*outPath, cfg, client, results); err != nil {
		fail("write report: %v", err)
	}
	abs, _ := filepath.Abs(*outPath)
	fmt.Printf("\n✓ Wrote %s\n", abs)
}

func runProbe(c *livebox.Client, p probe, timeout time.Duration, maxBody int) result {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	raw, err := c.RawCall(ctx, p.service, p.method, p.params)
	took := time.Since(start)

	r := result{probe: p, took: took}
	if err != nil {
		r.status = "TRANSPORT_ERROR"
		r.errMsg = err.Error()
		return r
	}

	// Parse the envelope to figure out whether the box returned an error.
	// The Funbox uses two error shapes:
	//   1. {"errors":[{error,description,info}], "status":null}
	//   2. {"error": N, "description": "...", "info": "..."}   (no array)
	var env struct {
		Status json.RawMessage `json:"status"`
		Data   json.RawMessage `json:"data"`
		Result json.RawMessage `json:"result"`
		Errors []apiErr        `json:"errors"`
		// Top-level error shape #2.
		Error       int    `json:"error"`
		Description string `json:"description"`
		Info        string `json:"info"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		r.status = "BAD_JSON"
		r.errMsg = err.Error()
		r.body = truncate(string(raw), maxBody)
		r.bodyFull = string(raw)
		return r
	}

	if len(env.Errors) > 0 {
		e := env.Errors[0]
		r.status = classifyDenial(e.Error)
		r.errMsg = fmt.Sprintf("[%d] %s — %s", e.Error, e.Description, e.Info)
		r.body = pretty(raw, maxBody)
		r.bodyFull = string(raw)
		return r
	}
	if env.Error != 0 || env.Description != "" {
		r.status = classifyDenial(env.Error)
		r.errMsg = fmt.Sprintf("[%d] %s — %s", env.Error, env.Description, env.Info)
		r.body = pretty(raw, maxBody)
		r.bodyFull = string(raw)
		return r
	}

	r.ok = true
	r.status = "OK"
	r.body = pretty(raw, maxBody)
	r.bodyFull = string(raw)
	return r
}

type apiErr struct {
	Error       int    `json:"error"`
	Description string `json:"description"`
	Info        string `json:"info"`
}

// classifyDenial maps the box's numeric error codes to friendlier labels.
func classifyDenial(code int) string {
	switch code {
	case 13:
		return "DENIED"
	case 196618: // 0x30002: Object or parameter not found
		return "NOT_FOUND"
	case 196619: // 0x30003: Service unavailable
		return "UNAVAILABLE"
	default:
		return "ERROR"
	}
}

func printLive(r result, maxBody int) {
	icon := "✗"
	if r.ok {
		icon = "✓"
	}
	fmt.Printf("%s %-40s %-15s (%s)\n", icon, r.probe.name, r.status, r.took.Truncate(time.Millisecond))
	if !r.ok {
		fmt.Printf("   error: %s\n", truncate(r.errMsg, 200))
	}
	if r.body != "" {
		for _, line := range strings.Split(strings.TrimSpace(r.body), "\n") {
			fmt.Printf("   │ %s\n", line)
		}
	}
	fmt.Println()
}

func writeReport(path string, cfg *config.Config, client *livebox.Client, results []result) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Sysbus diagnose report\n\n")
	fmt.Fprintf(&b, "- Date: %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(&b, "- Host: `%s`\n", cfg.Host)
	fmt.Fprintf(&b, "- Username: `%s`\n", cfg.Username)
	fmt.Fprintf(&b, "- Groups (server-assigned): `%s`\n\n", client.Groups())

	okCount := 0
	for _, r := range results {
		if r.ok {
			okCount++
		}
	}

	fmt.Fprintf(&b, "## Summary: %d / %d OK\n\n", okCount, len(results))
	fmt.Fprintln(&b, "| Probe | Status | Took | Note |")
	fmt.Fprintln(&b, "|-------|--------|------|------|")
	for _, r := range results {
		note := ""
		if !r.ok {
			note = truncate(r.errMsg, 80)
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n",
			r.probe.name, r.status, r.took.Truncate(time.Millisecond), escape(note))
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Per-probe details")
	for _, r := range results {
		fmt.Fprintln(&b)
		fmt.Fprintf(&b, "### %s — %s\n\n", r.probe.name, r.status)
		if r.probe.expect != "" {
			fmt.Fprintf(&b, "**Expected:** %s\n\n", r.probe.expect)
		}
		fmt.Fprintf(&b, "**Service:** `%s`  \n", r.probe.service)
		fmt.Fprintf(&b, "**Method:** `%s`  \n", r.probe.method)
		if len(r.probe.params) > 0 {
			pp, _ := json.MarshalIndent(r.probe.params, "", "  ")
			fmt.Fprintf(&b, "**Params:**\n```json\n%s\n```\n\n", pp)
		}
		fmt.Fprintf(&b, "**Took:** %s\n\n", r.took.Truncate(time.Millisecond))
		if !r.ok {
			fmt.Fprintf(&b, "**Error:** `%s`\n\n", r.errMsg)
		}
		if r.bodyFull != "" {
			body := r.bodyFull
			if len(body) > 6000 {
				body = body[:6000] + "\n... (truncated)"
			}
			body = prettyOrRaw(body)
			fmt.Fprintf(&b, "**Response:**\n\n```json\n%s\n```\n", body)
		}
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// ---------- helpers ----------

func matchTags(probeTags []string, only, skip map[string]bool) bool {
	for _, t := range probeTags {
		if skip[strings.ToLower(t)] {
			return false
		}
	}
	if len(only) == 0 {
		return true
	}
	for _, t := range probeTags {
		if only[strings.ToLower(t)] {
			return true
		}
	}
	return false
}

func splitTags(s string) map[string]bool {
	out := map[string]bool{}
	for _, t := range strings.Split(s, ",") {
		t = strings.TrimSpace(strings.ToLower(t))
		if t != "" {
			out[t] = true
		}
	}
	return out
}

func pretty(raw json.RawMessage, max int) string {
	return truncate(prettyOrRaw(string(raw)), max)
}

func prettyOrRaw(s string) string {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err == nil {
		out, err := json.MarshalIndent(v, "", "  ")
		if err == nil {
			// Sort top-level keys for deterministic output.
			sortKeysIfMap(v)
			return string(out)
		}
	}
	return s
}

func sortKeysIfMap(v any) {
	// json.MarshalIndent already sorts map keys, so this is a no-op
	// placeholder kept in case we later want custom field ordering.
	_ = sort.Sort
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func escape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", "\\|"), "\n", " ")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "diagnose: "+format+"\n", args...)
	os.Exit(1)
}
