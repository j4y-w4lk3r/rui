// login-dump performs a single sysbus login against the box and prints
// EVERYTHING the box returned: full status line, every response header,
// and the entire response body (pretty-printed as JSON if applicable).
//
// Use this when you suspect the box's login response carries auth state
// we're not parsing today (extra tokens, role info, capability list, etc.).
//
//	go run ./cmd/login-dump
//	go run ./cmd/login-dump -app so_sdkut
//	go run ./cmd/login-dump -out login.json
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
	"net/http/httputil"
	"os"
	"time"

	"github.com/j4y-w4lk3r/rui/internal/config"
)

func main() {
	envPath := flag.String("env", ".env", "path to .env file")
	app := flag.String("app", "webui", "applicationName parameter for createContext")
	outPath := flag.String("out", "", "also write the raw JSON response to this file")
	dump := flag.Bool("dump", true, "print full request + response with httputil.DumpRequest/DumpResponse")
	flag.Parse()

	cfg, err := config.Load(*envPath)
	if err != nil {
		die("config: %v", err)
	}

	base := "http://" + cfg.Host
	body, _ := json.Marshal(map[string]any{
		"service": "sah.Device.Information",
		"method":  "createContext",
		"parameters": map[string]any{
			"applicationName": *app,
			"username":        cfg.Username,
			"password":        cfg.Password,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/ws", bytes.NewReader(body))
	if err != nil {
		die("build request: %v", err)
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,pl;q=0.8")
	req.Header.Set("Authorization", "X-Sah-Login")
	req.Header.Set("Content-Type", "application/x-sah-ws-4-call+json")
	req.Header.Set("Origin", base)
	req.Header.Set("Referer", base+"/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	if *dump {
		raw, _ := httputil.DumpRequestOut(req, true)
		fmt.Printf("══════════════ REQUEST ══════════════\n%s\n", raw)
	}

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		die("send: %v", err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)

	if *dump {
		// We already consumed the body, so manually print headers + body.
		fmt.Printf("\n══════════════ RESPONSE ══════════════\n")
		fmt.Printf("%s %s\n", resp.Proto, resp.Status)
		for k, vs := range resp.Header {
			for _, val := range vs {
				fmt.Printf("%s: %s\n", k, val)
			}
		}
		fmt.Println()
	}

	// Pretty-print the body if it's JSON.
	var v any
	if err := json.Unmarshal(rawBody, &v); err == nil {
		pretty, _ := json.MarshalIndent(v, "", "  ")
		fmt.Printf("%s\n", pretty)
	} else {
		fmt.Printf("%s\n", rawBody)
	}

	// Also show all cookies the jar received (in case Set-Cookie is hidden).
	if u := req.URL; u != nil {
		cookies := jar.Cookies(u)
		if len(cookies) > 0 {
			fmt.Printf("\n══════════════ COOKIES SET ══════════════\n")
			for _, c := range cookies {
				fmt.Printf("  %s=%s (Domain=%s Path=%s)\n", c.Name, c.Value, c.Domain, c.Path)
			}
		} else {
			fmt.Printf("\n(no cookies set by the server)\n")
		}
	}

	if *outPath != "" {
		if err := os.WriteFile(*outPath, rawBody, 0o644); err != nil {
			die("write %s: %v", *outPath, err)
		}
		fmt.Printf("\n→ wrote raw body to %s\n", *outPath)
	}
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "login-dump: "+format+"\n", args...)
	os.Exit(1)
}
