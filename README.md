# rui

A small **Bubble Tea** TUI for managing home routers from the terminal.

```
┌──────────────┐  ┌───────────────────────────────────────────┐
│ ▸ Overview   │  │ System                                    │
│   Devices    │  │ Model        LiveboxFibre                 │
│   Wi-Fi      │  │ Software     SG40.B-7.30.x                │
│   Actions    │  │ Uptime       12d 04h 11m                  │
│              │  │                                           │
│ ↑/↓ move     │  │ WAN                                       │
│ enter open   │  │ Link state   up                           │
│ r  refresh   │  │ Public IP    90.84.x.x                    │
│ q  quit      │  │ ...                                       │
└──────────────┘  └───────────────────────────────────────────┘
✓ Connected to 192.168.1.1                       session 0m12s
```

`rui` (router-UI) is a single-binary terminal client that talks to your
home router's admin API and gives you a tabbed dashboard for the things
you actually care about: **system info**, **connected devices**, **Wi-Fi
toggle**, and **reboot / Wi-Fi-cycle actions**.

## Supported routers

| Vendor                          | Status              | Module                    |
| ------------------------------- | ------------------- | ------------------------- |
| Orange Livebox 4 / 5 / 6        | ✅ shipping today   | `internal/livebox/`       |
| Funbox 7 (Polish, Arcadyan ARF) | ✅ shipping today   | `internal/livebox/` (same sysbus protocol, stricter role mapping — see "Notes" below) |
| **Play home / Hub** (PL)        | ⏭ next on the list  | (planned `internal/play/`) |
| Fritz!Box (AVM)                 | ⏭ roadmap          | (planned `internal/fritz/`) |

The package layout is intentionally pluggable: each vendor lives in its
own subpackage under `internal/`, the TUI consumes whichever client the
config picks. Adding a new vendor is "implement the client interface +
register it" — no other plumbing needed.

If you want to help bring up a new vendor, the `cmd/capture` and
`cmd/diagnose` debug tools (see "Reverse-engineering toolkit" below)
are the same ones that produced the existing Livebox client.

## Install

### Homebrew (macOS, Linux)

```bash
brew install j4y-w4lk3r/rui/rui
```

### AUR (Arch Linux)

```bash
yay -S rui-bin                # prebuilt binary from the GitHub release
# or:
paru -S rui-bin
```

### From source

```bash
go install github.com/j4y-w4lk3r/rui/cmd/rui@latest
```

### Pre-built binaries

Grab the right tarball for your platform from the
[releases page](https://github.com/j4y-w4lk3r/rui/releases) — `darwin-arm64`,
`darwin-amd64`, `linux-arm64`, `linux-amd64` are all built per release.

## First run

1. Drop your router admin credentials into a `.env` file (gitignored):

   ```env
   username=admin
   password=your-router-admin-password
   ROUTER_HOST=192.168.1.1     # optional; defaults to 192.168.1.1
   ```

2. Run it:

   ```bash
   rui                         # uses .env in $PWD
   rui -env path/to/other.env  # different file
   rui -debug 2>debug.log      # log every HTTP request/response
   ```

## Keys

| Key       | Action                                                       |
| --------- | ------------------------------------------------------------ |
| `↑` / `↓` | Move in the side menu                                        |
| `enter`   | Open the selected pane                                       |
| `r`       | Refresh the current pane                                     |
| `t`       | Toggle Wi-Fi (on the Wi-Fi pane)                             |
| `b`       | Reboot the router (on Actions pane)                          |
| `y`       | Copy selected device's **IP** to clipboard (Devices tab)     |
| `Y`       | Copy selected device's **MAC** to clipboard (Devices tab)    |
| `q`       | Quit                                                         |

`y` / `Y` work both in the device list and inside the device detail
view, so once you've found `192.168.1.250 Mikro-Tik` (or any device)
you can paste it straight into `ssh`, `ping`, `mitmproxy`, etc.

## Notes

The Livebox client talks to the **sysbus** JSON API at
`http://<host>/ws`. Tested with Livebox 4 / 5 / 6 firmwares. The Polish
Funbox 7 (Arcadyan `ARF7-…`) speaks the same protocol but its admin role
mapping is stricter — if calls come back `Permission denied`, see the
`cmd/lab` tool below to reverse-engineer the exact request shape the
web UI uses.

If a call fails with `Permission denied`, double-check that the account
in `.env` actually has admin rights — the regular "user" account on
these boxes can only read a subset of the API.

## Reverse-engineering toolkit

`rui` ships **only the TUI binary** via brew/AUR, but the source tree
also contains a small toolkit that was used to build the Livebox client
and that's invaluable for bringing up new vendors. They are *not*
packaged — install the source repo and `go run` them:

```bash
git clone https://github.com/j4y-w4lk3r/rui && cd rui
```

### `cmd/probe`: one-shot API call

```bash
go run ./cmd/probe -- Devices get
go run ./cmd/probe -- NMC getWANStatus
go run ./cmd/probe -- TopologyDiagnostics buildTopology '{"SendXmlFile":false}'
```

Logs in (printing the `groups=` field — that's your role) and prints
the pretty-printed JSON envelope from the box.

### `cmd/lab`: try N login strategies, print a permission matrix

When `cmd/diagnose` shows many `DENIED` calls but the browser succeeds
with the same credentials, the question becomes: *what does the browser
do that we don't?* `lab` is a permutation tester that runs ~16 different
login flows (different headers, warm-ups, post-login sequences,
applicationNames) and prints a matrix of which canary calls each
unlocked.

```bash
go run ./cmd/lab
go run ./cmd/lab -only browser-mimic,post-getState
go run ./cmd/lab -verbose 2> lab.log
```

The matrix at the end shows variants vs canaries:

```
variant                    groups          DeviceInfo NMC.getWAN NMC.Wifi   ...
baseline-no-headers        http,admin      ✓          ✓          ✗          ...
baseline                   http,admin      ✓          ✓          ✗          ...
warm-events                http,admin      ✓          ✓          ✗          ...
post-browser-seq           http,admin      ✓          ✓          ✗          ...
browser-mimic              http,admin      ✓          ✓          ✓          ...
```

If any row has more ✓ than `baseline`, that combination of knobs is the
"unlock". Each run also writes a full markdown report at
`reports/lab-<timestamp>.md`.

### `cmd/diagnose`: probe a battery of endpoints, get a report

Logs in once, then calls every known sysbus endpoint and writes a
markdown report classifying each as OK / DENIED / TRANSPORT_ERROR. Use
this to figure out which calls work for the current session without
rebuilding the TUI.

```bash
go run ./cmd/diagnose                       # probe everything
go run ./cmd/diagnose -only wifi,devices    # filter by tag
go run ./cmd/diagnose -skip voice,iot       # skip tags
go run ./cmd/diagnose -debug 2> debug.log
```

Each run writes `reports/diagnose-<timestamp>.md` with:
- the role the box assigned to the session (`groups:`),
- a pass/fail table for every probe,
- the full JSON response (or error) for each one.

Add new endpoints to the `catalog` slice in `cmd/diagnose/main.go` if
you spot something interesting in a network capture.

### `cmd/capture`: record what the browser actually does

Launches a real Chrome via the DevTools Protocol, auto-fills the login
form using credentials from `.env`, and records every request the
browser makes while you click around. When you Ctrl+C it writes three
files into a fresh timestamped subdirectory of `captures/`:

- `capture.json` — full structured dump
- `capture.har`  — HAR 1.2, importable into Chrome DevTools or any HAR viewer
- `capture.md`   — human-readable summary highlighting POST/JSON traffic

```bash
go run ./cmd/capture
go run ./cmd/capture -url http://192.168.1.1
go run ./cmd/capture -no-auto-login
go run ./cmd/capture -host-only=false
go run ./cmd/capture -out my-trace
```

Workflow:
1. Run the command. Chrome opens, navigates to the router, and (if it
   can find the form) submits your credentials.
2. In the browser, click around — at minimum visit "Connected devices"
   and "System info". Every JSON request / response is recorded.
3. Press **Ctrl+C** in the terminal. The three files are written into
   `captures/<timestamp>/`.

Open `capture.md` to see the login request and the actual sysbus calls
the firmware makes; that tells you which `service` / `method` /
`parameters` to use in `internal/livebox/api.go` (or your new
`internal/<vendor>/api.go`).

### `cmd/devices`, `cmd/login-dump`

Smaller helpers — `devices` prints the device list as a wide table,
`login-dump` does a single login and dumps the cookies/headers so you
can paste them into `curl`.

### `cmd/mitm/dump.py`

A mitmproxy addon (Python) for capturing traffic when you can't or
won't use chromedp. Mostly archived; `cmd/capture` covers the same
ground without an MITM proxy in the path.

## Development

```bash
go build ./...                       # everything compiles
go test -race ./...                  # tests + race detector
go run ./cmd/rui -debug 2> debug.log # iterate on the TUI

goreleaser release --snapshot --clean   # local dry-run release
```

CI runs `go vet`, `go test`, and `goreleaser check` on every push to
main and every PR. Tags (`v*`) trigger
`.github/workflows/release.yml`, which:
1. cross-builds the binary for darwin/linux × amd64/arm64,
2. uploads tarballs + checksums to the GitHub release,
3. pushes the auto-rendered formula to
   [`j4y-w4lk3r/homebrew-rui`](https://github.com/j4y-w4lk3r/homebrew-rui).

To bump AUR after a release:

```bash
./arch/aur-bump.sh 0.1.0
```

## Sibling projects

- [`ykw`](https://github.com/j4y-w4lk3r/ykw) — multi-recipient YubiKey
  OpenPGP workflow CLI.
- `bbm` — Backblaze B2 manager, planned.

## License

[MIT](./LICENSE)
