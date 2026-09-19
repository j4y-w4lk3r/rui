package livebox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// CallVariant describes one transport attempt for a sysbus/sah API call.
type CallVariant struct {
	Name       string
	HTTPMethod string
	Path       string
	ContentTyp string
	Body       []byte
}

// CallResult is the outcome of one variant attempt.
type CallResult struct {
	Variant    CallVariant
	StatusCode int
	Body       json.RawMessage
	Err        error
}

func (c *Client) probeVariants(ctx context.Context, service, method string, params map[string]any) []CallResult {
	if params == nil {
		params = map[string]any{}
	}
	env, _ := json.Marshal(map[string]any{
		"service": service, "method": method, "parameters": params,
	})
	paramsOnly, _ := json.Marshal(map[string]any{"parameters": params})

	slashPath := "/sysbus/" + strings.ReplaceAll(service, ".", "/") + "/" + method
	colonPath := fmt.Sprintf("/sysbus/%s:%s", service, method)

	variants := []CallVariant{
		{"ws-envelope", http.MethodPost, "/ws", "application/x-sah-ws-4-call+json", env},
		{"ws-envelope-v1", http.MethodPost, "/ws", "application/x-sah-ws-1-call+json; charset=UTF-8", env},
		{"sysbus-colon-json", http.MethodPost, colonPath, "application/json", paramsOnly},
		{"sysbus-colon-sah1", http.MethodPost, colonPath, "application/x-sah-ws-1-call+json; charset=UTF-8", paramsOnly},
		{"sysbus-colon-envelope", http.MethodPost, colonPath, "application/x-sah-ws-4-call+json", env},
		{"sysbus-slash-json", http.MethodPost, slashPath, "application/json", paramsOnly},
		{"sysbus-slash-envelope", http.MethodPost, slashPath, "application/x-sah-ws-4-call+json", env},
	}

	// Legacy Livebox path without sah. prefix if applicable
	if strings.HasPrefix(service, "sah.") {
		legacy := strings.TrimPrefix(service, "sah.")
		legacyColon := fmt.Sprintf("/sysbus/%s:%s", legacy, method)
		variants = append(variants, CallVariant{
			"sysbus-legacy-colon", http.MethodPost, legacyColon, "application/json", paramsOnly,
		})
	}

	var out []CallResult
	for _, v := range variants {
		raw, code, err := c.doVariant(ctx, v)
		out = append(out, CallResult{Variant: v, StatusCode: code, Body: raw, Err: err})
	}
	return out
}

func (c *Client) doVariant(ctx context.Context, v CallVariant) (json.RawMessage, int, error) {
	u := c.baseURL + v.Path
	req, err := http.NewRequestWithContext(ctx, v.HTTPMethod, u, bytes.NewReader(v.Body))
	if err != nil {
		return nil, 0, err
	}
	if v.ContentTyp != "" {
		req.Header.Set("Content-Type", v.ContentTyp)
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("X-Context", c.ctxID)
	req.Header.Set("Authorization", "X-Sah "+c.ctxID)
	req.Header.Set("X-Prototype-Version", "1.7")
	c.setBrowserHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	c.captureSetCookies(resp)
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return raw, resp.StatusCode, nil
}

// ProbeCall tries all transport variants for any service/method pair.
func (c *Client) ProbeCall(ctx context.Context, service, method string, params map[string]any) []CallResult {
	return c.probeVariants(ctx, service, method, params)
}

// ProbeSah tries all transport variants for a sah.* reference.
func (c *Client) ProbeSah(ctx context.Context, ref string, params map[string]any) []CallResult {
	svc, meth, ok := ParseSahRef(ref)
	if !ok {
		return []CallResult{{Err: fmt.Errorf("bad ref %q", ref)}}
	}
	return c.probeVariants(ctx, svc, meth, params)
}

// BestResult returns first variant without API error in body.
func BestResult(results []CallResult) (CallResult, bool) {
	for _, r := range results {
		if r.Err != nil || r.StatusCode != http.StatusOK {
			continue
		}
		if APIErrorDescription(r.Body) == "" {
			return r, true
		}
	}
	return CallResult{}, false
}

// APIErrorDescription extracts first API error description from envelope.
func APIErrorDescription(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "empty body"
	}
	var env struct {
		Errors []struct {
			Description string `json:"description"`
			Info        string `json:"info"`
		} `json:"errors"`
		Result *struct {
			Errors []struct {
				Description string `json:"description"`
				Info        string `json:"info"`
			} `json:"errors"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return ""
	}
	if len(env.Errors) > 0 {
		return formatAPIErr(env.Errors[0].Description, env.Errors[0].Info)
	}
	if env.Result != nil && len(env.Result.Errors) > 0 {
		return formatAPIErr(env.Result.Errors[0].Description, env.Result.Errors[0].Info)
	}
	return ""
}

func formatAPIErr(desc, info string) string {
	if info != "" {
		return desc + " — " + info
	}
	return desc
}

// SysbusPost calls POST /sysbus/{service}:{method} with {"parameters":...}.
func (c *Client) SysbusPost(ctx context.Context, service, method string, params map[string]any) (json.RawMessage, error) {
	results := c.probeVariants(ctx, service, method, params)
	if r, ok := BestResult(results); ok {
		return r.Body, nil
	}
	if len(results) > 0 {
		last := results[0]
		return last.Body, fmt.Errorf("%s: HTTP %d %s", last.Variant.Name, last.StatusCode, APIErrorDescription(last.Body))
	}
	return nil, fmt.Errorf("no variants")
}

// ParseSahRef splits "sah.HomeNetwork.DhcpServerIpv4.listStaticLeases" into service + method.
func ParseSahRef(ref string) (service, method string, ok bool) {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, "sah.") {
		return "", "", false
	}
	i := strings.LastIndex(ref, ".")
	if i <= 4 {
		return "", "", false
	}
	return ref[:i], ref[i+1:], true
}

// EscapeSysbusPath URL-encodes a sysbus path segment (unused helper for future).
func EscapeSysbusPath(p string) string {
	return url.PathEscape(p)
}
