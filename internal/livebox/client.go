// Package livebox is a minimal client for the Orange Livebox "sysbus" JSON API.
//
// The Livebox exposes a single endpoint at http://<host>/ws that accepts JSON
// envelopes of the form:
//
//	{"service": "...", "method": "...", "parameters": {...}}
//
// Authentication uses createContext on sah.Device.Information; the response
// sets a session cookie that must be sent on all subsequent calls along with
// an X-Context header.
package livebox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"
)

// userAgent mirrors what a recent Chrome on macOS sends. Some Funbox/Livebox
// firmwares hand out a lower-privileged session to "non-browser" clients, so
// we lie about being Chrome on every request.
const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36"

// Client is a Livebox/Funbox sysbus client. Use New to construct one and
// Login before issuing any other call.
type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client
	ctxID    string
	groups   string // role(s) returned by createContext, e.g. "admin"
	debug    bool   // when true, dump every request/response to stderr

	// sessionCookies are the cookies the box set during login that we must
	// echo on every subsequent request. We store them ourselves because
	// Go's net/http/cookiejar follows RFC 6265 strictly and silently
	// rejects cookie names containing "/" (which the Funbox 7 firmware
	// uses, e.g. "51c31d85/sessid" and "sah/contextId"). Without these
	// cookies the box hands out a guest session even though the login
	// response claims groups="http,admin", which is the root cause of
	// the "Permission denied" results on privileged services.
	sessionCookies []rawCookie
}

// rawCookie is a name/value pair we manage ourselves, bypassing cookiejar.
type rawCookie struct {
	name, value string
}

// setBrowserHeaders adds the headers that the Funbox 7 web UI sends on every
// /ws request, including our manually-tracked session cookies (because Go's
// cookiejar can't store them — see Client.sessionCookies).
func (c *Client) setBrowserHeaders(req *http.Request) {
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,pl;q=0.8")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	req.Header.Set("Origin", c.baseURL)
	req.Header.Set("Referer", c.baseURL+"/")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")

	if cookie := c.cookieHeader(); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
}

// cookieHeader builds the "Cookie:" header value from our session cookies.
func (c *Client) cookieHeader() string {
	if len(c.sessionCookies) == 0 {
		return ""
	}
	parts := make([]string, 0, len(c.sessionCookies))
	for _, ck := range c.sessionCookies {
		parts = append(parts, ck.name+"="+ck.value)
	}
	return strings.Join(parts, "; ")
}

// captureSetCookies parses Set-Cookie headers from a response and stores any
// new cookies (replacing existing ones by name). Unlike net/http/cookiejar
// this is permissive about cookie name characters — necessary because the
// Funbox firmware uses "/" in names.
func (c *Client) captureSetCookies(resp *http.Response) {
	for _, raw := range resp.Header.Values("Set-Cookie") {
		name, value := parseSetCookie(raw)
		if name == "" {
			continue
		}
		// Replace any existing cookie of the same name.
		replaced := false
		for i, ck := range c.sessionCookies {
			if ck.name == name {
				c.sessionCookies[i].value = value
				replaced = true
				break
			}
		}
		if !replaced {
			c.sessionCookies = append(c.sessionCookies, rawCookie{name, value})
		}
		c.logf("  cookie set: %s=%s\n", name, truncate(value, 32))
	}
}

// parseSetCookie extracts the name and value from a Set-Cookie header value.
// Permissive — does not validate the name against RFC 6265.
func parseSetCookie(raw string) (name, value string) {
	parts := strings.SplitN(raw, ";", 2)
	if len(parts) == 0 {
		return "", ""
	}
	kv := strings.SplitN(strings.TrimSpace(parts[0]), "=", 2)
	if len(kv) != 2 {
		return "", ""
	}
	return strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1])
}

// addBrowserSyntheticCookies adds the cookies the browser's JavaScript sets
// client-side after login (sah/contextId, UILang, accept-language) that the
// box may also check. These are not set via Set-Cookie — the browser's UI
// JS writes them with document.cookie.
func (c *Client) addBrowserSyntheticCookies() {
	// sah/contextId mirrors the contextID we already send via X-Context.
	// Some firmware paths check the cookie variant.
	c.upsertCookie("sah/contextId", c.ctxID)
	c.upsertCookie("UILang", "en")

	// The accept-language cookie is namespaced by an ETag-like prefix
	// (e.g. "51c31d85/accept-language"). If we already received that prefix
	// from a Set-Cookie, mirror it.
	for _, ck := range c.sessionCookies {
		if i := strings.IndexByte(ck.name, '/'); i > 0 && strings.HasSuffix(ck.name, "/sessid") {
			prefix := ck.name[:i]
			c.upsertCookie(prefix+"/accept-language", "en-GB")
			break
		}
	}
}

func (c *Client) upsertCookie(name, value string) {
	for i, ck := range c.sessionCookies {
		if ck.name == name {
			c.sessionCookies[i].value = value
			return
		}
	}
	c.sessionCookies = append(c.sessionCookies, rawCookie{name, value})
}

// warmUp does the same prep dance a browser does before logging in:
//  1. GET / so any cookies/state get established;
//  2. POST /ws with an unauthenticated "events" subscription, which on some
//     Funbox firmwares seems to "tag" the connection as a real browser.
//
// Both calls have their own tight timeouts because the events POST is a
// long-poll that never naturally closes. Errors here are non-fatal — we log
// them in debug mode and continue.
func (c *Client) warmUp(ctx context.Context) {
	c.doGet(ctx, c.baseURL+"/", 1500*time.Millisecond, "GET / (warm-up)")

	subBody, _ := json.Marshal(map[string]any{
		"events": []string{
			"NMC", "Scheduler", "PnP", "ZWave",
			"Devices.Device", "RuleEngine", "PasswordRecovery",
		},
	})
	c.doShortPost(ctx, subBody, "application/x-sah-event-4-call+json",
		400*time.Millisecond, "POST /ws (events warm-up)", "")
}

// subscribeEvents does the AUTHENTICATED event subscription the browser
// issues right after login. We send-and-forget; the response body is a
// long-poll stream so we don't read it.
func (c *Client) subscribeEvents(ctx context.Context) {
	body, _ := json.Marshal(map[string]any{
		"events": []string{
			"NMC", "Scheduler", "PnP", "ZWave",
			"Devices.Device", "RuleEngine",
		},
		"channelid": 1,
	})
	c.doShortPost(ctx, body, "application/x-sah-event-4-call+json",
		400*time.Millisecond, "POST /ws (events sub, post-login)", c.ctxID)
}

// doGet performs a GET with its own deadline; errors are logged + discarded.
func (c *Client) doGet(ctx context.Context, url string, timeout time.Duration, label string) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	c.setBrowserHeaders(req)
	c.logf("→ %s\n", label)
	resp, err := c.http.Do(req)
	if err != nil {
		c.logf("← %s err=%v\n", label, err)
		return
	}
	defer resp.Body.Close()
	c.captureSetCookies(resp)
	io.Copy(io.Discard, resp.Body)
	c.logf("← %s HTTP %d\n", label, resp.StatusCode)
}

// doShortPost is a fire-and-forget POST with its own deadline. If ctxID is
// non-empty we include it as auth. The response body is discarded after a
// short read so a long-poll stream can't block us.
func (c *Client) doShortPost(ctx context.Context, body []byte, contentType string, timeout time.Duration, label, ctxID string) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, c.baseURL+"/ws", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Sah-Request-Type", "idle")
	if ctxID != "" {
		req.Header.Set("X-Context", ctxID)
		req.Header.Set("Authorization", "X-Sah "+ctxID)
	}
	c.setBrowserHeaders(req)
	c.logf("→ %s\n", label)
	resp, err := c.http.Do(req)
	if err != nil {
		c.logf("← %s err=%v\n", label, err)
		return
	}
	c.captureSetCookies(resp)
	go func() {
		defer resp.Body.Close()
		// Read up to 4 KiB so chunked encoding can release the connection
		// promptly; whatever's left will be discarded when we close.
		io.CopyN(io.Discard, resp.Body, 4096)
	}()
}

// Groups returns the role string the box assigned at login (e.g. "admin").
// Empty if login hasn't happened or the response didn't include it.
func (c *Client) Groups() string { return c.groups }

// Username returns the account name used at login.
func (c *Client) Username() string { return c.username }

// SetDebug toggles request/response logging on stderr.
func (c *Client) SetDebug(on bool) { c.debug = on }

// New constructs a Livebox client. host can be an IP, hostname or full URL.
func New(host, username, password string) (*Client, error) {
	base, err := normalizeBaseURL(host)
	if err != nil {
		return nil, err
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("cookie jar: %w", err)
	}

	return &Client{
		baseURL:  base,
		username: username,
		password: password,
		http: &http.Client{
			Jar:     jar,
			Timeout: 10 * time.Second,
		},
	}, nil
}

func normalizeBaseURL(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", errors.New("empty host")
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}
	u, err := url.Parse(host)
	if err != nil {
		return "", fmt.Errorf("invalid host %q: %w", host, err)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// envelope is the JSON wrapper sent on every sysbus call.
type envelope struct {
	Service    string         `json:"service"`
	Method     string         `json:"method"`
	Parameters map[string]any `json:"parameters"`
}

// response is the generic shape returned by /ws. The interesting payload sits
// under "data" (the schema varies by call) and "status" is true on success.
type response struct {
	Status json.RawMessage `json:"status"`
	Data   json.RawMessage `json:"data"`
	Result json.RawMessage `json:"result"`
	Errors []apiError      `json:"errors"`
}

type apiError struct {
	Error       int    `json:"error"`
	Description string `json:"description"`
	Info        string `json:"info"`
}

// Login authenticates against the box and stores the session context.
//
// We try several applicationName values because Funbox/Livebox firmwares
// disagree on which one yields admin rights. "webui" is what the official
// web UI uses (verified on Funbox 7 via traffic capture); the rest are
// fallbacks for other firmwares.
func (c *Client) Login(ctx context.Context) error {
	// Some Funbox firmwares only hand out the "real" admin session to
	// connections that look like a freshly-opened browser tab. Mimic that
	// by loading "/" and opening an event channel before the createContext.
	c.warmUp(ctx)

	apps := []string{"webui", "so_sdkut", "Web", "WebUI"}
	var lastErr error
	for _, app := range apps {
		err := c.loginAs(ctx, app)
		if err == nil {
			// Now do the post-login event subscription the browser does.
			c.subscribeEvents(ctx)
			return nil
		}
		lastErr = err
		// If the box explicitly rejected the credentials we shouldn't keep
		// hammering it with the same wrong password.
		if isCredentialError(err) {
			return err
		}
	}
	return lastErr
}

func (c *Client) loginAs(ctx context.Context, appName string) error {
	payload := envelope{
		Service: "sah.Device.Information",
		Method:  "createContext",
		Parameters: map[string]any{
			"applicationName": appName,
			"username":        c.username,
			"password":        c.password,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal login: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/ws", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-sah-ws-4-call+json")
	req.Header.Set("Authorization", "X-Sah-Login")
	c.setBrowserHeaders(req)

	c.logf("→ POST /ws (login, app=%s)\n", appName)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("send login: %w", err)
	}
	defer resp.Body.Close()

	// IMPORTANT: capture cookies manually before reading the body — Go's
	// cookiejar will silently drop names containing "/", which the Funbox
	// firmware uses ("51c31d85/sessid"). Without this, every subsequent
	// authenticated call will be downgraded to a guest session.
	c.captureSetCookies(resp)

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read login response: %w", err)
	}
	c.logf("← HTTP %d %s\n", resp.StatusCode, truncate(string(raw), 400))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	var parsed struct {
		Data struct {
			ContextID string `json:"contextID"`
			Username  string `json:"username"`
			Groups    string `json:"groups"`
		} `json:"data"`
		Errors []apiError `json:"errors"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("decode login: %w (body=%s)", err, truncate(string(raw), 200))
	}
	if len(parsed.Errors) > 0 {
		return fmt.Errorf("login rejected: %s", parsed.Errors[0].Description)
	}
	if parsed.Data.ContextID == "" {
		return fmt.Errorf("login: missing contextID (body=%s)", truncate(string(raw), 200))
	}

	c.ctxID = parsed.Data.ContextID
	c.groups = parsed.Data.Groups

	// Now that we have the contextID, add the synthetic cookies that the
	// web UI's JavaScript writes client-side.
	c.addBrowserSyntheticCookies()
	c.logf("  cookie header now: %s\n", truncate(c.cookieHeader(), 200))
	return nil
}

func isCredentialError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "invalid") ||
		strings.Contains(s, "wrong") ||
		strings.Contains(s, "credential") ||
		strings.Contains(s, "password")
}

func (c *Client) logf(format string, args ...any) {
	if !c.debug {
		return
	}
	fmt.Fprintf(os.Stderr, "[livebox] "+format, args...)
}

// RawCall is the unparsed counterpart of call: it returns the full raw JSON
// envelope from the box without inspecting the "errors" array. Useful for the
// probe CLI when we explicitly want to *see* the error structure.
func (c *Client) RawCall(ctx context.Context, service, method string, params map[string]any) (json.RawMessage, error) {
	if c.ctxID == "" {
		return nil, errors.New("not logged in: call Login() first")
	}
	if params == nil {
		params = map[string]any{}
	}
	body, err := json.Marshal(envelope{Service: service, Method: method, Parameters: params})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/ws", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-sah-ws-4-call+json")
	req.Header.Set("X-Context", c.ctxID)
	req.Header.Set("Authorization", "X-Sah "+c.ctxID)
	c.setBrowserHeaders(req)

	c.logf("→ %s.%s %s\n", service, method, truncate(string(body), 300))
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	c.captureSetCookies(resp)
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	c.logf("← %s.%s HTTP %d %s\n", service, method, resp.StatusCode, truncate(string(raw), 400))
	return raw, nil
}

// call performs an authenticated sysbus request and unmarshals the raw
// response into out (the full envelope, so callers can pick data or result).
func (c *Client) call(ctx context.Context, service, method string, params map[string]any) (*response, error) {
	if c.ctxID == "" {
		return nil, errors.New("not logged in: call Login() first")
	}
	if params == nil {
		params = map[string]any{}
	}

	body, err := json.Marshal(envelope{Service: service, Method: method, Parameters: params})
	if err != nil {
		return nil, fmt.Errorf("marshal call %s.%s: %w", service, method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/ws", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-sah-ws-4-call+json")
	req.Header.Set("X-Context", c.ctxID)
	req.Header.Set("Authorization", "X-Sah "+c.ctxID)
	c.setBrowserHeaders(req)

	c.logf("→ %s.%s %s\n", service, method, truncate(string(body), 300))
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send %s.%s: %w", service, method, err)
	}
	defer resp.Body.Close()
	c.captureSetCookies(resp)

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	c.logf("← %s.%s HTTP %d %s\n", service, method, resp.StatusCode, truncate(string(raw), 400))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s.%s HTTP %d: %s", service, method, resp.StatusCode, truncate(string(raw), 200))
	}

	var out response
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode %s.%s: %w (body=%s)", service, method, err, truncate(string(raw), 200))
	}
	if len(out.Errors) > 0 {
		return nil, fmt.Errorf("%s.%s: %s", service, method, out.Errors[0].Description)
	}
	return &out, nil
}

// IsPermissionDenied reports whether the router rejected a sysbus call because
// the logged-in session lacks the required role (common with non-admin
// accounts and some Funbox firmwares even after a successful login).
func IsPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "permission denied")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
