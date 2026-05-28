// capture launches a real Chrome via the DevTools Protocol, navigates to the
// router, auto-fills the login form using credentials from .env, and records
// every network request/response while you click around in the browser.
//
// When you Ctrl+C (or close the browser) it writes:
//
//   - capture.json: full raw dump of every captured request
//   - capture.har:  same data in HAR 1.2 format (viewable in Chrome DevTools,
//     Firefox, https://toolbox.googleapps.com/apps/har_analyzer/, etc.)
//   - capture.md:   human-readable summary highlighting POST/JSON traffic
//
// Usage:
//
//	go run ./cmd/capture
//	go run ./cmd/capture -url http://192.168.1.1 -out ./trace
//	go run ./cmd/capture -no-auto-login  # type credentials yourself
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"github.com/j4y-w4lk3r/rui/internal/config"
)

type captured struct {
	RequestID       string            `json:"requestId"`
	URL             string            `json:"url"`
	Method          string            `json:"method"`
	RequestHeaders  map[string]string `json:"requestHeaders"`
	PostData        string            `json:"postData,omitempty"`
	StatusCode      int64             `json:"statusCode,omitempty"`
	StatusText      string            `json:"statusText,omitempty"`
	MIMEType        string            `json:"mimeType,omitempty"`
	ResponseHeaders map[string]string `json:"responseHeaders,omitempty"`
	ResponseBody    string            `json:"responseBody,omitempty"`
	Base64Body      bool              `json:"base64Body,omitempty"`
	StartedAt       time.Time         `json:"startedAt"`
	FinishedAt      time.Time         `json:"finishedAt,omitempty"`
	ResourceType    string            `json:"resourceType,omitempty"`
}

func main() {
	envPath := flag.String("env", ".env", "path to .env file")
	startURL := flag.String("url", "", "starting URL (defaults to http://<ROUTER_HOST>)")
	outDir := flag.String("out", "",
		"directory to write capture artifacts into "+
			"(default: captures/<timestamp>/)")
	noAuto := flag.Bool("no-auto-login", false, "don't try to auto-fill the login form")
	hostOnly := flag.Bool("host-only", true, "only record requests to the router host (off = record everything)")
	headless := flag.Bool("headless", false, "run Chrome in headless mode (no visible window)")
	timeout := flag.Duration("timeout", 0, "auto-stop after this duration (0 = wait for Ctrl+C)")
	flag.Parse()

	if *outDir == "" {
		*outDir = filepath.Join("captures", time.Now().Format("2006-01-02_15-04-05"))
	}

	cfg, err := config.Load(*envPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}

	target := *startURL
	if target == "" {
		target = "http://" + cfg.Host
	}
	parsed, err := url.Parse(target)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad url:", err)
		os.Exit(1)
	}
	hostname := parsed.Hostname()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir:", err)
		os.Exit(1)
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", *headless),
		chromedp.Flag("disable-cache", true),
		chromedp.Flag("disable-application-cache", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.IgnoreCertErrors,
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	// Optional auto-stop deadline.
	if *timeout > 0 {
		var cancelT context.CancelFunc
		ctx, cancelT = context.WithTimeout(ctx, *timeout)
		defer cancelT()
	}

	store := &captureStore{
		hostOnly: *hostOnly,
		host:     hostname,
		byID:     map[string]*captured{},
	}

	chromedp.ListenTarget(ctx, func(ev any) {
		switch ev := ev.(type) {

		case *network.EventRequestWillBeSent:
			store.beginRequest(ev)

		case *network.EventResponseReceived:
			store.observeResponse(ev)

		case *network.EventLoadingFinished:
			id := string(ev.RequestID)
			if !store.tracks(id) {
				return
			}
			// Fetch the body asynchronously so we don't deadlock the listener.
			go func() {
				body, err := fetchBody(ctx, ev.RequestID)
				if err != nil {
					return
				}
				store.attachBody(id, body)
			}()

		case *network.EventLoadingFailed:
			store.markFailed(string(ev.RequestID), ev.ErrorText)
		}
	})

	fmt.Println("⚙  Starting Chrome... (this can take a few seconds the first time)")
	if err := chromedp.Run(ctx,
		network.Enable(),
		page.Enable(),
		chromedp.Navigate(target),
	); err != nil {
		fmt.Fprintln(os.Stderr, "navigate:", err)
		os.Exit(1)
	}
	fmt.Printf("→ Opened %s\n", target)

	// Chrome usually opens an extra blank tab on launch; close it so the user
	// only sees the page chromedp is controlling.
	closeBlankTabs(ctx)

	if !*noAuto {
		fmt.Println("↪  Attempting auto-login with credentials from .env...")
		if err := autoLogin(ctx, cfg.Username, cfg.Password); err != nil {
			fmt.Fprintf(os.Stderr, "   auto-login: %v (you can log in manually)\n", err)
		} else {
			fmt.Println("✓  Auto-login submitted.")
		}
	}

	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Println(" Recording network traffic.")
	fmt.Println(" In the browser, click \"Connected devices\" and \"System info\"")
	fmt.Println(" (Podłączone urządzenia / Informacje systemowe), then come")
	fmt.Println(" back here and press Ctrl+C to save the capture.")
	fmt.Println("════════════════════════════════════════════════════════════")

	// Wait for Ctrl+C or context cancellation.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sigCh:
		fmt.Println("\n⏹  Stopping capture...")
	case <-ctx.Done():
		fmt.Println("\n⏹  Browser closed or timed out.")
	}

	requests := store.snapshot()
	fmt.Printf("   captured %d requests\n", len(requests))

	if err := writeJSON(filepath.Join(*outDir, "capture.json"), requests); err != nil {
		fmt.Fprintln(os.Stderr, "write json:", err)
	}
	if err := writeHAR(filepath.Join(*outDir, "capture.har"), requests); err != nil {
		fmt.Fprintln(os.Stderr, "write har:", err)
	}
	if err := writeMarkdown(filepath.Join(*outDir, "capture.md"), requests, target); err != nil {
		fmt.Fprintln(os.Stderr, "write md:", err)
	}

	fmt.Printf("✓  Wrote capture into %s/\n   capture.json\n   capture.har\n   capture.md\n", *outDir)
}

// captureStore is goroutine-safe storage for in-flight + finished requests.
type captureStore struct {
	hostOnly bool
	host     string

	mu    sync.Mutex
	byID  map[string]*captured
	order []string
}

func (s *captureStore) tracks(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.byID[id]
	return ok
}

func (s *captureStore) beginRequest(ev *network.EventRequestWillBeSent) {
	if s.hostOnly {
		if u, err := url.Parse(ev.Request.URL); err != nil || u.Hostname() != s.host {
			return
		}
	}
	id := string(ev.RequestID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.byID[id]; !exists {
		s.order = append(s.order, id)
	}
	s.byID[id] = &captured{
		RequestID:      id,
		URL:            ev.Request.URL,
		Method:         ev.Request.Method,
		RequestHeaders: headerMap(ev.Request.Headers),
		PostData:       joinPostData(ev.Request.PostDataEntries),
		StartedAt:      time.Now(),
		ResourceType:   string(ev.Type),
	}
}

func joinPostData(entries []*network.PostDataEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.Bytes)
	}
	return b.String()
}

func (s *captureStore) observeResponse(ev *network.EventResponseReceived) {
	id := string(ev.RequestID)
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.byID[id]
	if !ok {
		return
	}
	r.StatusCode = int64(ev.Response.Status)
	r.StatusText = ev.Response.StatusText
	r.MIMEType = ev.Response.MimeType
	r.ResponseHeaders = headerMap(ev.Response.Headers)
}

func (s *captureStore) attachBody(id string, body responseBody) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.byID[id]
	if !ok {
		return
	}
	r.ResponseBody = body.text
	r.Base64Body = body.base64
	r.FinishedAt = time.Now()
}

func (s *captureStore) markFailed(id, errText string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.byID[id]; ok {
		r.StatusText = "FAILED: " + errText
		r.FinishedAt = time.Now()
	}
}

func (s *captureStore) snapshot() []*captured {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*captured, 0, len(s.order))
	for _, id := range s.order {
		if r, ok := s.byID[id]; ok {
			out = append(out, r)
		}
	}
	return out
}

type responseBody struct {
	text   string
	base64 bool
}

func fetchBody(ctx context.Context, id network.RequestID) (responseBody, error) {
	body, err := network.GetResponseBody(id).Do(ctx)
	if err != nil {
		return responseBody{}, err
	}
	return responseBody{text: string(body), base64: false}, nil
}

func headerMap(h network.Headers) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		out[k] = fmt.Sprint(v)
	}
	return out
}

// closeBlankTabs closes any target whose URL is empty, "about:blank", or a
// chrome:// internal page, so the user only sees the page chromedp drove.
func closeBlankTabs(ctx context.Context) {
	targets, err := chromedp.Targets(ctx)
	if err != nil {
		return
	}
	myTargetID := chromedp.FromContext(ctx).Target.TargetID
	for _, t := range targets {
		if t.TargetID == myTargetID {
			continue
		}
		if t.Type != "page" {
			continue
		}
		url := t.URL
		if url == "" || url == "about:blank" || strings.HasPrefix(url, "chrome://") {
			_ = target.CloseTarget(t.TargetID).Do(ctx)
		}
	}
}

// autoLogin tries the most common selectors for username/password fields and
// submits the form. Handles Polish/French/English button labels and falls
// back to pressing Enter on the password field if nothing else works.
func autoLogin(ctx context.Context, username, password string) error {
	// Give the page up to 15s to render its login form.
	loginCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	js := fmt.Sprintf(`(() => {
		const pw = document.querySelector('input[type=password]');
		if (!pw) return 'no-password-field';

		let user =
			document.querySelector('input[name=username]') ||
			document.querySelector('input[name=user]') ||
			document.querySelector('input[name=login]') ||
			document.querySelector('input[id=username]') ||
			document.querySelector('input[id=user]');
		if (!user) {
			const inputs = [...document.querySelectorAll('input')];
			const idx = inputs.indexOf(pw);
			for (let i = idx - 1; i >= 0; i--) {
				const t = (inputs[i].type || '').toLowerCase();
				if (t !== 'password' && t !== 'submit' && t !== 'button' && t !== 'hidden') {
					user = inputs[i]; break;
				}
			}
		}

		const fire = (el, v) => {
			const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
			setter.call(el, v);
			el.dispatchEvent(new Event('input', {bubbles:true}));
			el.dispatchEvent(new Event('change', {bubbles:true}));
			el.dispatchEvent(new Event('blur',  {bubbles:true}));
		};
		if (user) fire(user, %q);
		fire(pw, %q);

		// Wait a tick so any reactive frameworks pick up the values.
		const submit = () => {
			// 1. Explicit submit buttons.
			let btn =
				document.querySelector('button[type=submit]') ||
				document.querySelector('input[type=submit]');
			// 2. By id/class/data attributes hinting at "login".
			if (!btn) {
				btn = document.querySelector(
					'button[id*=login i], button[id*=sign i], button[id*=submit i],' +
					'button[class*=login i], button[class*=sign i], button[class*=submit i],' +
					'button[data-test*=login i], button[data-test*=submit i],' +
					'a[id*=login i], a[class*=login i]'
				);
			}
			// 3. By visible text: "Zaloguj" (PL), "Login", "Sign in", "Connexion" (FR),
			//    "Submit", "OK", "Enter", "Continuer".
			if (!btn) {
				const labels = ['zaloguj','log in','login','sign in','signin',
				                'connexion','submit','ok','enter','continuer','continue'];
				const all = [...document.querySelectorAll('button, a, input[type=button], [role=button]')];
				btn = all.find(el => {
					const t = (el.innerText || el.value || el.textContent || '').trim().toLowerCase();
					return labels.some(l => t === l || t.startsWith(l + ' ') || t.endsWith(' ' + l) || t.includes(l));
				});
			}
			// 4. If the form has a single button, click it.
			if (!btn && pw.form) {
				const formBtns = pw.form.querySelectorAll('button, input[type=button]');
				if (formBtns.length === 1) btn = formBtns[0];
			}
			if (btn) { btn.click(); return 'clicked:' + (btn.innerText || btn.value || btn.tagName); }
			// 5. Fall back to Enter on the password field.
			pw.dispatchEvent(new KeyboardEvent('keydown', {key:'Enter', code:'Enter', keyCode:13, which:13, bubbles:true}));
			pw.dispatchEvent(new KeyboardEvent('keypress',{key:'Enter', code:'Enter', keyCode:13, which:13, bubbles:true}));
			pw.dispatchEvent(new KeyboardEvent('keyup',  {key:'Enter', code:'Enter', keyCode:13, which:13, bubbles:true}));
			if (pw.form) { try { pw.form.submit(); return 'form-submit'; } catch(e) {} }
			return 'enter-key';
		};
		// Give frameworks a moment to register the typed values, then submit.
		return new Promise(resolve => setTimeout(() => resolve(submit()), 250));
	})()`, username, password)

	var result string
	err := chromedp.Run(loginCtx,
		chromedp.WaitVisible("input[type=password]", chromedp.ByQuery),
		chromedp.Evaluate(js, &result, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}),
	)
	if err != nil {
		return err
	}
	if result == "no-password-field" {
		return fmt.Errorf("could not find password field")
	}
	fmt.Printf("   → submit strategy: %s\n", result)
	return nil
}

// ---------- output writers ----------

func writeJSON(path string, requests []*captured) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(requests)
}

// harFile is a minimal subset of the HAR 1.2 schema. Only the fields we
// actually populate are included.
type harFile struct {
	Log harLog `json:"log"`
}

type harLog struct {
	Version string     `json:"version"`
	Creator harCreator `json:"creator"`
	Entries []harEntry `json:"entries"`
}

type harCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type harEntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            float64     `json:"time"`
	Request         harRequest  `json:"request"`
	Response        harResponse `json:"response"`
	Cache           struct{}    `json:"cache"`
	Timings         harTimings  `json:"timings"`
}

type harRequest struct {
	Method      string      `json:"method"`
	URL         string      `json:"url"`
	HTTPVersion string      `json:"httpVersion"`
	Headers     []harHeader `json:"headers"`
	QueryString []harHeader `json:"queryString"`
	PostData    *harPost    `json:"postData,omitempty"`
	HeadersSize int         `json:"headersSize"`
	BodySize    int         `json:"bodySize"`
}

type harResponse struct {
	Status      int64       `json:"status"`
	StatusText  string      `json:"statusText"`
	HTTPVersion string      `json:"httpVersion"`
	Headers     []harHeader `json:"headers"`
	Cookies     []harHeader `json:"cookies"`
	Content     harContent  `json:"content"`
	RedirectURL string      `json:"redirectURL"`
	HeadersSize int         `json:"headersSize"`
	BodySize    int         `json:"bodySize"`
}

type harHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harPost struct {
	MIMEType string `json:"mimeType"`
	Text     string `json:"text"`
}

type harContent struct {
	Size     int    `json:"size"`
	MIMEType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
	Encoding string `json:"encoding,omitempty"`
}

type harTimings struct {
	Send    float64 `json:"send"`
	Wait    float64 `json:"wait"`
	Receive float64 `json:"receive"`
}

func writeHAR(path string, requests []*captured) error {
	har := harFile{
		Log: harLog{
			Version: "1.2",
			Creator: harCreator{Name: "orange-tui/capture", Version: "0.1"},
			Entries: make([]harEntry, 0, len(requests)),
		},
	}
	for _, r := range requests {
		entry := harEntry{
			StartedDateTime: r.StartedAt.UTC().Format(time.RFC3339Nano),
			Time:            float64(r.FinishedAt.Sub(r.StartedAt).Milliseconds()),
			Request: harRequest{
				Method:      r.Method,
				URL:         r.URL,
				HTTPVersion: "HTTP/1.1",
				Headers:     mapToHARHeaders(r.RequestHeaders),
				QueryString: queryHeaders(r.URL),
				HeadersSize: -1,
				BodySize:    len(r.PostData),
			},
			Response: harResponse{
				Status:      r.StatusCode,
				StatusText:  r.StatusText,
				HTTPVersion: "HTTP/1.1",
				Headers:     mapToHARHeaders(r.ResponseHeaders),
				Content: harContent{
					Size:     len(r.ResponseBody),
					MIMEType: r.MIMEType,
					Text:     r.ResponseBody,
				},
				HeadersSize: -1,
				BodySize:    len(r.ResponseBody),
			},
		}
		if r.PostData != "" {
			entry.Request.PostData = &harPost{
				MIMEType: r.RequestHeaders["Content-Type"],
				Text:     r.PostData,
			}
		}
		if r.Base64Body {
			entry.Response.Content.Encoding = "base64"
		}
		har.Log.Entries = append(har.Log.Entries, entry)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(har)
}

func mapToHARHeaders(m map[string]string) []harHeader {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]harHeader, 0, len(keys))
	for _, k := range keys {
		out = append(out, harHeader{Name: k, Value: m[k]})
	}
	return out
}

func queryHeaders(rawURL string) []harHeader {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	var out []harHeader
	for k, vs := range u.Query() {
		for _, v := range vs {
			out = append(out, harHeader{Name: k, Value: v})
		}
	}
	return out
}

// writeMarkdown produces a human-readable summary that's easy to share.
func writeMarkdown(path string, requests []*captured, target string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Network capture (%s)\n\n", target)
	fmt.Fprintf(&b, "Recorded %d requests at %s.\n\n", len(requests), time.Now().Format(time.RFC3339))

	fmt.Fprintln(&b, "## Summary")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| # | Method | Status | URL |")
	fmt.Fprintln(&b, "|---|--------|--------|-----|")
	for i, r := range requests {
		fmt.Fprintf(&b, "| %d | %s | %d | `%s` |\n", i+1, r.Method, r.StatusCode, truncate(r.URL, 90))
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Interesting requests (POST / JSON)")
	fmt.Fprintln(&b)
	for i, r := range requests {
		if !isInteresting(r) {
			continue
		}
		fmt.Fprintf(&b, "### %d. %s %s\n\n", i+1, r.Method, r.URL)
		fmt.Fprintf(&b, "**Status:** %d %s  \n", r.StatusCode, r.StatusText)
		fmt.Fprintf(&b, "**Content-Type:** %s\n\n", r.MIMEType)

		fmt.Fprintln(&b, "**Request headers:**")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "```")
		for _, h := range mapToHARHeaders(r.RequestHeaders) {
			fmt.Fprintf(&b, "%s: %s\n", h.Name, h.Value)
		}
		fmt.Fprintln(&b, "```")
		fmt.Fprintln(&b)

		if r.PostData != "" {
			fmt.Fprintln(&b, "**Request body:**")
			fmt.Fprintln(&b)
			fmt.Fprintln(&b, "```")
			fmt.Fprintln(&b, prettyJSONOrRaw(r.PostData))
			fmt.Fprintln(&b, "```")
			fmt.Fprintln(&b)
		}

		fmt.Fprintln(&b, "**Response headers:**")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "```")
		for _, h := range mapToHARHeaders(r.ResponseHeaders) {
			fmt.Fprintf(&b, "%s: %s\n", h.Name, h.Value)
		}
		fmt.Fprintln(&b, "```")
		fmt.Fprintln(&b)

		if r.ResponseBody != "" {
			fmt.Fprintln(&b, "**Response body:**")
			fmt.Fprintln(&b)
			fmt.Fprintln(&b, "```")
			fmt.Fprintln(&b, prettyJSONOrRaw(r.ResponseBody))
			fmt.Fprintln(&b, "```")
			fmt.Fprintln(&b)
		}
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func isInteresting(r *captured) bool {
	if strings.EqualFold(r.Method, "POST") {
		return true
	}
	mt := strings.ToLower(r.MIMEType)
	return strings.Contains(mt, "json") || strings.Contains(mt, "xml")
}

func prettyJSONOrRaw(s string) string {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err == nil {
		out, err := json.MarshalIndent(v, "", "  ")
		if err == nil {
			return string(out)
		}
	}
	if len(s) > 4000 {
		return s[:4000] + "\n... (truncated)"
	}
	return s
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
