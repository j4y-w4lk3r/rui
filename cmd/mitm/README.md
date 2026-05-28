# MITM capture (full request + response bodies)

`chromedp` couldn't capture response bodies for chunked /ws calls — it sees
the request and headers but not the JSON the box sent back. `mitmproxy`
(already installed on your Mac) can. This directory contains a tiny script
that runs mitmproxy as a *reverse proxy* in front of the router so you can
browse the UI normally and we get a clean recording of everything.

## How it works

mitmproxy runs locally as `http://127.0.0.1:8888`, forwards every request to
`http://192.168.1.1`, and records both directions to a flow file. The browser
sees `127.0.0.1:8888` (no proxy config needed), the router sees `192.168.1.1`,
and `mitm.py` writes a pretty-printed JSON dump per request to
`mitm-<timestamp>.jsonl`.

## Quick start

In one terminal:

```bash
cd orange
mitmdump --mode reverse:http://192.168.1.1 --listen-port 8888 -s ./cmd/mitm/dump.py
```

In your browser, visit:

```
http://127.0.0.1:8888/
```

Log in, click around. When you're done, hit Ctrl+C in the mitmdump terminal.

## Output files

Three files are written per session, all stamped with the start time:

| File                       | What it contains                                              |
|----------------------------|---------------------------------------------------------------|
| `mitm-<ts>.jsonl`          | One JSON record per flow, with full headers + bodies (structured, machine-readable). |
| `mitm-<ts>.log`            | Plain-text mirror of the live console output (one line per flow). |
| `mitm-<ts>.summary.md`     | Pretty markdown summary written on shutdown — sysbus calls in a table plus a full flow index. |

The `.log` file is the answer to "I want all the mitm output saved somewhere
I can grep later" — it's literally the same lines mitmdump printed live, in
order. The `.summary.md` is the friendly version you skim *after* a session
to remember what got captured.

## Why use this?

This is how we discovered the **session-cookie bug**: the box sends
`Set-Cookie: 51c31d85/sessid=…` on login, but Go's `net/http/cookiejar`
silently drops it because the cookie name contains a `/` (RFC 6265 forbids
that, browsers don't care). Without that cookie our subsequent requests were
downgraded to a guest session even though the login response said
`groups="http,admin"`. The fix lives in `internal/livebox/client.go` —
`captureSetCookies` parses Set-Cookie ourselves and `addBrowserSyntheticCookies`
mirrors the cookies the UI's JavaScript sets client-side
(`sah/contextId`, `UILang`, `<etag>/accept-language`).

mitmproxy is also the only way to read response bodies from chunked /ws
calls — `chromedp` couldn't capture those.
