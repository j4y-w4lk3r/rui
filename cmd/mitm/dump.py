"""mitmproxy addon that dumps every flow to disk with full bodies.

Three files are written per session, all stamped with the start time:

    mitm-<ts>.jsonl      One JSON record per flow, with full headers + bodies.
                         This is the structured, machine-readable log.
    mitm-<ts>.log        Human-readable mirror of the live console output
                         (`[status] METHOD url`). Handy when you just want
                         to scroll through what happened.
    mitm-<ts>.summary.md Pretty markdown summary written when mitmdump exits
                         (Ctrl-C): one-line-per-flow with timing and a quick
                         JSON peek so you can diff sessions at a glance.

Usage:
    mitmdump --mode reverse:http://192.168.1.1 --listen-port 8888 -s dump.py
"""

import json
import time
import os
from mitmproxy import http


_TS = time.strftime("%Y-%m-%d_%H-%M-%S")
_OUT_DIR = "reports"
_JSONL = None  # structured log (one record per flow)
_LOG = None    # human-readable console mirror
_FLOWS = []    # tiny in-memory index used for the summary file


def _open_log():
    """Open (lazily) and return (jsonl_file, log_file)."""
    global _JSONL, _LOG
    if _JSONL is None:
        os.makedirs(_OUT_DIR, exist_ok=True)
        jname = os.path.join(_OUT_DIR, f"mitm-{_TS}.jsonl")
        lname = os.path.join(_OUT_DIR, f"mitm-{_TS}.log")
        _JSONL = open(jname, "w", buffering=1)
        _LOG = open(lname, "w", buffering=1)
        banner = f"[mitm-dump] writing JSONL → {os.path.abspath(jname)}\n[mitm-dump] writing log   → {os.path.abspath(lname)}"
        print(banner)
        _LOG.write(banner + "\n")
    return _JSONL, _LOG


def _emit(line: str):
    """Print to stdout AND mirror to the .log file."""
    print(line)
    if _LOG is not None:
        _LOG.write(line + "\n")


def _try_decode_json(blob: bytes):
    """Return a Python object if the body parses as JSON, else None."""
    if not blob:
        return None
    try:
        return json.loads(blob.decode("utf-8", errors="replace"))
    except Exception:
        return None


def response(flow: http.HTTPFlow):
    jsonl, _ = _open_log()

    req = flow.request
    resp = flow.response

    req_body = req.get_text(strict=False) or ""
    resp_body = resp.get_text(strict=False) if resp else ""
    req_json = _try_decode_json(req.content)
    resp_json = _try_decode_json(resp.content) if resp else None

    record = {
        "ts": time.time(),
        "request": {
            "method": req.method,
            "scheme": req.scheme,
            "host": req.host,
            "port": req.port,
            "path": req.path,
            "url": req.url,
            "headers": dict(req.headers.items()),
            "body": req_body,
            "body_json": req_json,
        },
        "response": {
            "status_code": resp.status_code if resp else None,
            "reason": resp.reason if resp else None,
            "headers": dict(resp.headers.items()) if resp else {},
            "body": resp_body,
            "body_json": resp_json,
        },
    }

    jsonl.write(json.dumps(record, ensure_ascii=False, default=str) + "\n")

    # Live progress (mirrored to .log).
    method = req.method
    url = req.url
    status = resp.status_code if resp else "—"
    ws = " /ws" if req.path.startswith("/ws") else ""
    _emit(f"  [{status}] {method:5} {url}{ws}")

    # Keep a tiny index for the summary; one-liners derived from sysbus calls
    # are vastly more readable than scrolling through 200+ asset GETs.
    service = method_name = None
    if isinstance(req_json, dict):
        service = req_json.get("service")
        method_name = req_json.get("method")
    _FLOWS.append({
        "ts": time.time(),
        "method": method,
        "status": status,
        "url": url,
        "service": service,
        "sysbus_method": method_name,
        "req_size": len(req_body),
        "resp_size": len(resp_body),
    })


def done():
    """Called by mitmproxy on shutdown (Ctrl-C). Emits a markdown summary."""
    if _JSONL is None:
        return
    try:
        _JSONL.close()
    except Exception:
        pass
    if _LOG is not None:
        try:
            _LOG.close()
        except Exception:
            pass

    sname = os.path.join(_OUT_DIR, f"mitm-{_TS}.summary.md")
    with open(sname, "w") as f:
        f.write(f"# mitm capture summary — {_TS}\n\n")
        f.write(f"Total flows: **{len(_FLOWS)}**\n\n")

        sysbus = [x for x in _FLOWS if x["service"]]
        if sysbus:
            f.write("## sysbus calls\n\n")
            f.write("| # | status | service | method | req | resp |\n")
            f.write("|---|--------|---------|--------|-----|------|\n")
            for i, fl in enumerate(sysbus, 1):
                f.write(f"| {i} | {fl['status']} | `{fl['service']}` | `{fl['sysbus_method']}` | {fl['req_size']}B | {fl['resp_size']}B |\n")
            f.write("\n")

        f.write("## all flows\n\n")
        f.write("| # | status | method | url |\n")
        f.write("|---|--------|--------|-----|\n")
        for i, fl in enumerate(_FLOWS, 1):
            short = fl["url"]
            if len(short) > 100:
                short = short[:97] + "..."
            f.write(f"| {i} | {fl['status']} | {fl['method']} | `{short}` |\n")

    print(f"[mitm-dump] wrote summary → {os.path.abspath(sname)}")
