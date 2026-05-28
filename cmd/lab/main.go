// lab is an experimental framework for figuring out which combination of
// headers / warm-up calls / post-login sequence unlocks the most sysbus
// endpoints on the Funbox 7. Each "variant" is a fresh HTTP client + cookie
// jar with its own configuration; we run them all sequentially and produce
// a matrix of canary call results so we can read off which variant wins.
//
// Usage:
//
//	go run ./cmd/lab
//	go run ./cmd/lab -only browser-mimic,events-after
//	go run ./cmd/lab -out lab.md
//	go run ./cmd/lab -verbose
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/j4y-w4lk3r/rui/internal/config"
)

// ----- request building blocks -----

type sysCall struct {
	service string
	method  string
	params  map[string]any
}

func (c sysCall) String() string {
	if len(c.params) == 0 {
		return c.service + "." + c.method
	}
	return c.service + "." + c.method + "(...)"
}

// callOutcome is the result of issuing a single sysbus call.
type callOutcome struct {
	statusCode int
	httpErr    error
	apiErr     string // populated when the box returns an error envelope
	bodyHead   string // first ~200 chars of body for the report
	took       time.Duration
}

func (o callOutcome) ok() bool { return o.statusCode == 200 && o.apiErr == "" && o.httpErr == nil }

func (o callOutcome) short() string {
	switch {
	case o.httpErr != nil:
		return "NETERR"
	case o.statusCode != 200:
		return fmt.Sprintf("HTTP%d", o.statusCode)
	case o.apiErr != "":
		return o.apiErr // typically "DENIED" or "[N] msg"
	}
	return "OK"
}

// ----- variant configuration -----

type variant struct {
	name string
	tags []string

	// Auth
	appName string

	// Header knobs
	noBrowserHeaders bool // when true, send only Content-Type + Authorization
	noOrigin         bool
	noXRequestedWith bool
	noSecFetch       bool
	noAcceptLanguage bool
	noAcceptEncoding bool
	extraHeaders     map[string]string

	// Pre-login warmup
	preLoginGetSlash    bool
	preLoginEventsUnauth bool

	// Post-login sequence: calls to make BEFORE we run the canaries.
	postLoginCalls   []sysCall
	postLoginEventSub bool
}

// canary calls we measure each variant against. Picked to span permission
// tiers — DeviceInfo is the easiest, TopologyDiagnostics the hardest.
var canaries = []sysCall{
	{"DeviceInfo", "get", nil},
	{"NMC", "getWANStatus", nil},
	{"NMC", "get", nil},
	{"NMC.Wifi", "get", nil},
	{"HTTPService", "getCurrentUser", nil},
	{"Manifests", "retrieve", map[string]any{"user": "admin", "option": "wm_settings"}},
	{"Devices", "get", nil},
	{"TopologyDiagnostics", "buildTopology", map[string]any{"SendXmlFile": false}},
}

// browser-observed post-login sequence (from the real capture).
var browserPostLoginSeq = []sysCall{
	{"UserInterface", "getState", nil},
	{"HTTPService", "getCurrentUser", nil},
	{"UserManagement", "getUser", map[string]any{"name": "admin"}},
	{"Manifests", "retrieve", map[string]any{"user": "admin", "option": "wm_settings"}},
}

func variants() []variant {
	return []variant{
		// --- isolating the effect of each knob ---
		{name: "baseline-no-headers", tags: []string{"baseline"}, appName: "webui", noBrowserHeaders: true},
		{name: "baseline", tags: []string{"baseline"}, appName: "webui"},

		{name: "no-origin", tags: []string{"hdr"}, appName: "webui", noOrigin: true},
		{name: "no-xrequested", tags: []string{"hdr"}, appName: "webui", noXRequestedWith: true},
		{name: "no-secfetch", tags: []string{"hdr"}, appName: "webui", noSecFetch: true},
		{name: "no-acceptlang", tags: []string{"hdr"}, appName: "webui", noAcceptLanguage: true},

		// --- pre-login warm-ups ---
		{name: "warm-get", tags: []string{"warmup"}, appName: "webui", preLoginGetSlash: true},
		{name: "warm-events", tags: []string{"warmup"}, appName: "webui", preLoginEventsUnauth: true},
		{name: "warm-both", tags: []string{"warmup"}, appName: "webui",
			preLoginGetSlash: true, preLoginEventsUnauth: true},

		// --- post-login orchestration ---
		{name: "post-getState", tags: []string{"postlogin"}, appName: "webui",
			postLoginCalls: []sysCall{{"UserInterface", "getState", nil}}},
		{name: "post-currentUser", tags: []string{"postlogin"}, appName: "webui",
			postLoginCalls: []sysCall{{"HTTPService", "getCurrentUser", nil}}},
		{name: "post-browser-seq", tags: []string{"postlogin"}, appName: "webui",
			postLoginCalls: browserPostLoginSeq},
		{name: "post-events", tags: []string{"postlogin"}, appName: "webui", postLoginEventSub: true},

		// --- combos that mirror the browser as closely as we can ---
		{name: "browser-mimic", tags: []string{"combo"}, appName: "webui",
			preLoginGetSlash: true, preLoginEventsUnauth: true,
			postLoginCalls: browserPostLoginSeq, postLoginEventSub: true},
		{name: "browser-mimic-no-events", tags: []string{"combo"}, appName: "webui",
			preLoginGetSlash: true,
			postLoginCalls: browserPostLoginSeq},

		// --- different applicationName tries ---
		{name: "app-sosdkut", tags: []string{"appname"}, appName: "so_sdkut"},
		{name: "app-Web", tags: []string{"appname"}, appName: "Web"},
	}
}

func main() {
	envPath := flag.String("env", ".env", "path to .env file")
	outPath := flag.String("out", "", "markdown report path (default: reports/lab-<timestamp>.md)")
	onlyStr := flag.String("only", "", "comma-separated variant names to run (empty = all)")
	verbose := flag.Bool("verbose", false, "log every HTTP request")
	timeout := flag.Duration("timeout", 5*time.Second, "per-call timeout")
	flag.Parse()

	if *outPath == "" {
		*outPath = filepath.Join("reports", "lab-"+time.Now().Format("2006-01-02_15-04-05")+".md")
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

	only := splitSet(*onlyStr)

	all := variants()
	var selected []variant
	for _, v := range all {
		if len(only) == 0 || only[v.name] {
			selected = append(selected, v)
		}
	}
	if len(selected) == 0 {
		fail("no variants matched -only=%q", *onlyStr)
	}

	fmt.Printf("→ Host: %s  user: %s\n", cfg.Host, cfg.Username)
	fmt.Printf("→ Running %d variants, %d canaries each\n\n", len(selected), len(canaries))

	var results []rowResult

	for _, v := range selected {
		fmt.Printf("══ %-30s ", v.name)
		row := rowResult{variant: v}

		ctx, cancel := context.WithTimeout(context.Background(), *timeout*time.Duration(len(canaries)+8))
		ctxID, groups, loginErr := runLogin(ctx, cfg, v, *verbose, *timeout)
		cancel()
		row.groups = groups
		if loginErr != nil {
			row.login = "LOGIN_FAIL: " + truncate(loginErr.Error(), 60)
			fmt.Printf("%s\n", row.login)
			results = append(results, row)
			continue
		}
		row.login = "OK"
		fmt.Printf("login OK groups=%q\n", groups)

		// Optional post-login orchestration (best-effort, errors ignored).
		for _, pc := range v.postLoginCalls {
			pctx, pcancel := context.WithTimeout(context.Background(), *timeout)
			_ = doSysCall(pctx, cfg.Host, v, ctxID, pc, *verbose)
			pcancel()
		}
		if v.postLoginEventSub {
			postLoginEventSub(cfg.Host, v, ctxID, *verbose)
			time.Sleep(150 * time.Millisecond) // let the box settle
		}

		// Run canaries.
		for _, c := range canaries {
			pctx, pcancel := context.WithTimeout(context.Background(), *timeout)
			o := doSysCall(pctx, cfg.Host, v, ctxID, c, *verbose)
			pcancel()
			row.outcomes = append(row.outcomes, o)
			icon := "✗"
			if o.ok() {
				icon = "✓"
			}
			fmt.Printf("   %s %-38s %s\n", icon, c.String(), o.short())
		}

		results = append(results, row)
		fmt.Println()
		// Be polite to the box between variants.
		time.Sleep(300 * time.Millisecond)
	}

	// ----- ASCII summary matrix -----
	fmt.Println()
	fmt.Println("══════════════════════════════════════════════════")
	fmt.Println("  Matrix (rows = variants, columns = canaries)")
	fmt.Println("══════════════════════════════════════════════════")
	header := fmt.Sprintf("%-26s %-15s", "variant", "groups")
	for _, c := range canaries {
		header += fmt.Sprintf(" %-10s", abbrev(c.String(), 10))
	}
	fmt.Println(header)
	fmt.Println(strings.Repeat("─", len(header)))
	for _, r := range results {
		line := fmt.Sprintf("%-26s %-15s", truncate(r.name, 26), truncate(r.groups, 15))
		if r.login != "OK" {
			line += " " + r.login
		} else {
			for _, o := range r.outcomes {
				cell := "·"
				if o.ok() {
					cell = "✓"
				} else if strings.HasPrefix(o.short(), "[13]") || o.short() == "DENIED" {
					cell = "✗"
				} else {
					cell = "?"
				}
				line += fmt.Sprintf(" %-10s", cell)
			}
		}
		fmt.Println(line)
	}
	fmt.Println()

	// Rank variants by how many canaries each unlocked.
	type scored struct {
		v     variant
		score int
		login string
	}
	var ranked []scored
	for _, r := range results {
		s := scored{v: r.variant, login: r.login}
		for _, o := range r.outcomes {
			if o.ok() {
				s.score++
			}
		}
		ranked = append(ranked, s)
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })

	fmt.Println("Ranking:")
	for _, s := range ranked {
		fmt.Printf("  %2d / %d  %s\n", s.score, len(canaries), s.v.name)
	}

	if err := writeReport(*outPath, cfg, selected, canaries, results); err != nil {
		fail("write report: %v", err)
	}
	fmt.Printf("\n✓ Report: %s\n", *outPath)
}

// ----- core HTTP machinery (no shared client.go — full control) -----

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36"

func newClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 10 * time.Second}
}

func setHeaders(req *http.Request, v variant, baseURL string) {
	if v.noBrowserHeaders {
		return
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Referer", baseURL+"/")
	req.Header.Set("User-Agent", userAgent)
	if !v.noAcceptLanguage {
		req.Header.Set("Accept-Language", "en-US,en;q=0.9,pl;q=0.8")
	}
	if !v.noAcceptEncoding {
		req.Header.Set("Accept-Encoding", "gzip, deflate")
	}
	if !v.noOrigin {
		req.Header.Set("Origin", baseURL)
	}
	if !v.noXRequestedWith {
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	if !v.noSecFetch {
		req.Header.Set("Sec-Fetch-Dest", "empty")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	for k, val := range v.extraHeaders {
		req.Header.Set(k, val)
	}
}

func runLogin(ctx context.Context, cfg *config.Config, v variant, verbose bool, timeout time.Duration) (ctxID string, groups string, err error) {
	baseURL := "http://" + cfg.Host
	client := newClient()

	// Optional warm-ups.
	if v.preLoginGetSlash {
		gctx, gc := context.WithTimeout(ctx, 1500*time.Millisecond)
		req, _ := http.NewRequestWithContext(gctx, http.MethodGet, baseURL+"/", nil)
		setHeaders(req, v, baseURL)
		if verbose {
			fmt.Fprintf(os.Stderr, "[%s] warm GET /\n", v.name)
		}
		if resp, err := client.Do(req); err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		gc()
	}
	if v.preLoginEventsUnauth {
		body, _ := json.Marshal(map[string]any{
			"events": []string{"NMC", "Scheduler", "PnP", "Devices.Device", "RuleEngine"},
		})
		fireAndForget(ctx, client, baseURL+"/ws", body,
			"application/x-sah-event-4-call+json", "", v, verbose,
			"warm events (unauth)")
	}

	// Actual login.
	loginBody, _ := json.Marshal(map[string]any{
		"service": "sah.Device.Information",
		"method":  "createContext",
		"parameters": map[string]any{
			"applicationName": v.appName,
			"username":        cfg.Username,
			"password":        cfg.Password,
		},
	})
	lctx, lcancel := context.WithTimeout(ctx, timeout)
	defer lcancel()
	req, _ := http.NewRequestWithContext(lctx, http.MethodPost, baseURL+"/ws", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/x-sah-ws-4-call+json")
	req.Header.Set("Authorization", "X-Sah-Login")
	setHeaders(req, v, baseURL)
	if verbose {
		fmt.Fprintf(os.Stderr, "[%s] login (app=%s)\n", v.name, v.appName)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	if verbose {
		fmt.Fprintf(os.Stderr, "[%s] login response headers:\n", v.name)
		for k, vs := range resp.Header {
			for _, val := range vs {
				fmt.Fprintf(os.Stderr, "    %s: %s\n", k, val)
			}
		}
		fmt.Fprintf(os.Stderr, "[%s] login response body (%d bytes):\n", v.name, len(raw))
		// Pretty-print if it's JSON.
		var any any
		if err := json.Unmarshal(raw, &any); err == nil {
			out, _ := json.MarshalIndent(any, "    ", "  ")
			fmt.Fprintf(os.Stderr, "    %s\n", string(out))
		} else {
			fmt.Fprintf(os.Stderr, "    %s\n", string(raw))
		}
	}

	var parsed struct {
		Data struct {
			ContextID string `json:"contextID"`
			Username  string `json:"username"`
			Groups    string `json:"groups"`
		} `json:"data"`
		Errors []apiErr `json:"errors"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", "", fmt.Errorf("decode login: %w (body=%s)", err, truncate(string(raw), 120))
	}
	if len(parsed.Errors) > 0 {
		return "", "", fmt.Errorf("server: %s", parsed.Errors[0].Description)
	}
	if parsed.Data.ContextID == "" {
		return "", "", fmt.Errorf("no contextID (body=%s)", truncate(string(raw), 120))
	}

	// Stash the client in a package-global so doSysCall can find it.
	clientByVariant[v.name] = client
	return parsed.Data.ContextID, parsed.Data.Groups, nil
}

// We use a tiny map to keep the cookie jar / connection pool consistent
// across all the calls for one variant.
var clientByVariant = map[string]*http.Client{}

func doSysCall(ctx context.Context, host string, v variant, ctxID string, c sysCall, verbose bool) callOutcome {
	baseURL := "http://" + host
	client := clientByVariant[v.name]
	if client == nil {
		client = newClient()
	}
	params := c.params
	if params == nil {
		params = map[string]any{}
	}
	body, _ := json.Marshal(map[string]any{
		"service":    c.service,
		"method":     c.method,
		"parameters": params,
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/ws", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-sah-ws-4-call+json")
	req.Header.Set("X-Context", ctxID)
	req.Header.Set("Authorization", "X-Sah "+ctxID)
	setHeaders(req, v, baseURL)

	if verbose {
		fmt.Fprintf(os.Stderr, "[%s] → %s\n", v.name, c.String())
	}
	t0 := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return callOutcome{httpErr: err, took: time.Since(t0)}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	took := time.Since(t0)

	out := callOutcome{statusCode: resp.StatusCode, took: took, bodyHead: truncate(string(raw), 200)}

	var env struct {
		Errors      []apiErr `json:"errors"`
		Error       int      `json:"error"`
		Description string   `json:"description"`
		Info        string   `json:"info"`
	}
	if err := json.Unmarshal(raw, &env); err == nil {
		if len(env.Errors) > 0 {
			e := env.Errors[0]
			if e.Error == 13 {
				out.apiErr = "DENIED"
			} else {
				out.apiErr = fmt.Sprintf("[%d]", e.Error)
			}
		} else if env.Error != 0 {
			out.apiErr = fmt.Sprintf("[%d]", env.Error)
		}
	}
	return out
}

func postLoginEventSub(host string, v variant, ctxID string, verbose bool) {
	baseURL := "http://" + host
	body, _ := json.Marshal(map[string]any{
		"events": []string{"NMC", "Scheduler", "PnP", "Devices.Device", "RuleEngine"},
		"channelid": 1,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	client := clientByVariant[v.name]
	if client == nil {
		return
	}
	fireAndForget(ctx, client, baseURL+"/ws", body,
		"application/x-sah-event-4-call+json", ctxID, v, verbose, "events sub (post-login)")
}

func fireAndForget(ctx context.Context, client *http.Client, url string, body []byte, contentType, ctxID string, v variant, verbose bool, label string) {
	cctx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		cancel()
		return
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Sah-Request-Type", "idle")
	if ctxID != "" {
		req.Header.Set("X-Context", ctxID)
		req.Header.Set("Authorization", "X-Sah "+ctxID)
	}
	setHeaders(req, v, "http://"+strings.SplitN(url, "/", 4)[2])
	if verbose {
		fmt.Fprintf(os.Stderr, "[%s] → %s\n", v.name, label)
	}
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		return
	}
	go func() {
		defer cancel()
		defer resp.Body.Close()
		io.CopyN(io.Discard, resp.Body, 4096)
	}()
}

// ----- output -----

type rowResult = struct {
	variant
	groups   string
	login    string
	outcomes []callOutcome
}

func writeReport(path string, cfg *config.Config, variants []variant, canaries []sysCall, results []rowResult) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Lab report\n\n")
	fmt.Fprintf(&b, "- Date: %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(&b, "- Host: `%s`\n", cfg.Host)
	fmt.Fprintf(&b, "- User: `%s`\n", cfg.Username)
	fmt.Fprintf(&b, "- Variants: %d\n", len(variants))
	fmt.Fprintf(&b, "- Canaries: %d\n\n", len(canaries))

	// Matrix.
	fmt.Fprintln(&b, "## Matrix")
	fmt.Fprintln(&b)
	fmt.Fprint(&b, "| variant | groups | login |")
	for _, c := range canaries {
		fmt.Fprintf(&b, " %s |", c.String())
	}
	fmt.Fprintln(&b)
	fmt.Fprint(&b, "|---|---|---|")
	for range canaries {
		fmt.Fprint(&b, "---|")
	}
	fmt.Fprintln(&b)
	for _, r := range results {
		fmt.Fprintf(&b, "| `%s` | `%s` | %s |", r.name, r.groups, r.login)
		for _, o := range r.outcomes {
			cell := "✗ " + o.short()
			if o.ok() {
				cell = "✓"
			}
			fmt.Fprintf(&b, " %s |", cell)
		}
		fmt.Fprintln(&b)
	}

	// Per-variant detail.
	fmt.Fprintln(&b, "\n## Per-variant details")
	fmt.Fprintln(&b)
	for _, r := range results {
		fmt.Fprintf(&b, "### `%s`\n\n", r.name)
		fmt.Fprintf(&b, "- App name: `%s`\n", r.appName)
		fmt.Fprintf(&b, "- Pre-login: getSlash=%t  eventsUnauth=%t\n", r.preLoginGetSlash, r.preLoginEventsUnauth)
		fmt.Fprintf(&b, "- Post-login event sub: %t\n", r.postLoginEventSub)
		if len(r.postLoginCalls) > 0 {
			var ss []string
			for _, c := range r.postLoginCalls {
				ss = append(ss, c.String())
			}
			fmt.Fprintf(&b, "- Post-login calls: %s\n", strings.Join(ss, " → "))
		}
		fmt.Fprintf(&b, "- Headers off: browser=%t origin=%t xreq=%t secfetch=%t accLang=%t accEnc=%t\n",
			r.noBrowserHeaders, r.noOrigin, r.noXRequestedWith, r.noSecFetch, r.noAcceptLanguage, r.noAcceptEncoding)
		fmt.Fprintf(&b, "- Login result: %s\n", r.login)
		fmt.Fprintf(&b, "- Server-assigned groups: `%s`\n\n", r.groups)
		if len(r.outcomes) > 0 {
			fmt.Fprintln(&b, "| canary | result | http | took | body |")
			fmt.Fprintln(&b, "|---|---|---|---|---|")
			for i, o := range r.outcomes {
				fmt.Fprintf(&b, "| `%s` | %s | %d | %s | <code>%s</code> |\n",
					canaries[i].String(), o.short(), o.statusCode, o.took.Truncate(time.Millisecond), escape(o.bodyHead))
			}
		}
		fmt.Fprintln(&b)
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// ----- misc -----

type apiErr struct {
	Error       int    `json:"error"`
	Description string `json:"description"`
	Info        string `json:"info"`
}

func splitSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, t := range strings.Split(s, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			out[t] = true
		}
	}
	return out
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

func abbrev(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Keep the suffix for distinguishability (e.g. .get vs .buildTopology).
	if n < 4 {
		return s[:n]
	}
	return s[:n-2] + ".."
}

func escape(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "<", "&lt;")
	return s
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "lab: "+format+"\n", args...)
	os.Exit(1)
}
