#!/usr/bin/env python3
"""Real-life soak test for all exporters.

Starts every exporter with docker compose (built from source), scrapes each
/metrics endpoint the way Prometheus does for --duration, samples container
resource usage, and then analyses the trends to spot leaks:

  * memory: docker memory usage, process RSS, Go heap in use
  * handles: goroutines, OS threads / PIDs, open file descriptors
  * cardinality: number of series per scrape
  * health: scrape failures, latency, redirect of / to /metrics,
            container exits / OOM kills

Stdlib only. Usage (from the repo root):

    python soak/soak.py                       # 30 minute run
    python soak/soak.py --duration 5m         # quick run
    python soak/soak.py --analyze soak/results/<run>   # re-analyse a run

Results go to soak/results/<timestamp>/ (report.html, report.md, CSVs, logs).
"""
from __future__ import annotations

import argparse
import csv
import gzip
import html
import http.client
import json
import re
import signal
import statistics
import subprocess
import sys
import threading
import time
from datetime import datetime
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
PROJECT = "exporrters-soak"
COMPOSE = ["docker", "compose", "-p", PROJECT,
           "-f", str(ROOT / "compose.yaml"), "-f", str(ROOT / "soak" / "compose.soak.yaml")]

# service -> host port (see compose.yaml)
TARGETS = {"mikrotik": 9436, "ping": 9427, "stock": 8087, "weather": 9080, "xiaomi": 8086}

# Self-metrics every exporter exposes through its Prometheus client library.
SELF_METRICS = {
    "rss_bytes": "process_resident_memory_bytes",
    "open_fds": "process_open_fds",
    "goroutines": "go_goroutines",
    "go_threads": "go_threads",
    "heap_inuse_bytes": "go_memstats_heap_inuse_bytes",
}

MIB = 1024 * 1024


# --------------------------------------------------------------------------
# helpers
# --------------------------------------------------------------------------

def parse_duration(s: str) -> float:
    m = re.fullmatch(r"(\d+(?:\.\d+)?)([smh]?)", s.strip())
    if not m:
        raise argparse.ArgumentTypeError(f"bad duration {s!r} (use e.g. 90s, 30m, 1h)")
    return float(m.group(1)) * {"": 1, "s": 1, "m": 60, "h": 3600}[m.group(2)]


def parse_size(s: str) -> float:
    m = re.match(r"([\d.]+)\s*([KMGT]?i?B)", s.strip())
    if not m:
        return float("nan")
    mult = {"B": 1, "KiB": 1024, "MiB": MIB, "GiB": 1024 * MIB, "TiB": 1024 ** 4,
            "kB": 1000, "KB": 1000, "MB": 1e6, "GB": 1e9, "TB": 1e12}[m.group(2)]
    return float(m.group(1)) * mult


def log(msg: str) -> None:
    print(f"[{datetime.now():%H:%M:%S}] {msg}", flush=True)


def compose(*args: str, check: bool = True, capture: bool = False) -> subprocess.CompletedProcess:
    return subprocess.run([*COMPOSE, *args], cwd=ROOT, check=check, text=True,
                          capture_output=capture)


def get(port: int, path: str, timeout: float, conn: http.client.HTTPConnection | None = None):
    """GET like Prometheus (keep-alive, gzip). Returns (status, headers, body, conn)."""
    if conn is None:
        conn = http.client.HTTPConnection("127.0.0.1", port, timeout=timeout)
    conn.request("GET", path, headers={
        "Accept": "text/plain;version=0.0.4;q=0.9,*/*;q=0.1",
        "Accept-Encoding": "gzip",
        "User-Agent": "exporrters-soak/1.0",
    })
    resp = conn.getresponse()
    body = resp.read()
    if resp.getheader("Content-Encoding") == "gzip":
        body = gzip.decompress(body)
    return resp.status, dict(resp.getheaders()), body, conn


SAMPLE = re.compile(r"^([a-zA-Z_:][a-zA-Z0-9_:]*)(\{[^}]*\})?\s+(\S+)")


def parse_metrics(text: str) -> tuple[int, dict[str, float]]:
    series = 0
    values: dict[str, float] = {}
    wanted = set(SELF_METRICS.values())
    for line in text.splitlines():
        if not line or line[0] == "#":
            continue
        series += 1
        m = SAMPLE.match(line)
        if m and m.group(1) in wanted and not m.group(2):
            try:
                values[m.group(1)] = float(m.group(3))
            except ValueError:
                pass
    return series, values


# --------------------------------------------------------------------------
# collectors
# --------------------------------------------------------------------------

SCRAPE_FIELDS = ["t", "service", "ok", "status", "error", "latency_ms", "bytes", "series",
                 *SELF_METRICS.keys()]
STATS_FIELDS = ["t", "service", "mem_bytes", "mem_limit_bytes", "cpu_pct", "pids", "net_in", "net_out"]
REDIRECT_FIELDS = ["t", "service", "ok", "status", "location", "error"]


class Recorder:
    def __init__(self, out: Path):
        self.lock = threading.Lock()
        self.files = {}
        self.writers = {}
        for name, fields in (("scrapes", SCRAPE_FIELDS), ("docker_stats", STATS_FIELDS),
                             ("redirects", REDIRECT_FIELDS)):
            f = open(out / f"{name}.csv", "w", newline="", encoding="utf-8")
            w = csv.DictWriter(f, fieldnames=fields)
            w.writeheader()
            self.files[name], self.writers[name] = f, w
        self.latest: dict[str, dict] = {}

    def write(self, kind: str, row: dict) -> None:
        with self.lock:
            self.writers[kind].writerow(row)
            self.files[kind].flush()
            self.latest[(kind, row["service"])] = row

    def close(self) -> None:
        for f in self.files.values():
            f.close()


def scrape_loop(service: str, port: int, args, t0: float, rec: Recorder, stop: threading.Event):
    conn = None
    next_at = time.monotonic()
    next_redirect = time.monotonic()
    while not stop.is_set():
        start = time.monotonic()
        row = {"t": round(time.time() - t0, 2), "service": service}
        try:
            status, _, body, conn = get(port, "/metrics", args.scrape_timeout, conn)
            series, values = parse_metrics(body.decode("utf-8", "replace"))
            row.update(ok=int(status == 200), status=status, error="", bytes=len(body), series=series)
            for key, metric in SELF_METRICS.items():
                row[key] = values.get(metric, "")
        except Exception as err:  # timeout, refused, reset ...
            row.update(ok=0, status="", error=type(err).__name__ + ": " + str(err)[:120])
            if conn is not None:
                conn.close()
            conn = None
        row["latency_ms"] = round((time.monotonic() - start) * 1000, 1)
        rec.write("scrapes", row)

        if time.monotonic() >= next_redirect:  # GET / must redirect to /metrics
            next_redirect += 60
            r = {"t": round(time.time() - t0, 2), "service": service}
            try:
                status, headers, _, c = get(port, "/", args.scrape_timeout)
                c.close()
                loc = headers.get("Location", "")
                r.update(ok=int(status in (301, 302, 307, 308) and loc.endswith("/metrics")),
                         status=status, location=loc, error="")
            except Exception as err:
                r.update(ok=0, status="", location="", error=type(err).__name__)
            rec.write("redirects", r)

        next_at += args.scrape_interval
        stop.wait(max(0.0, next_at - time.monotonic()))
    if conn is not None:
        conn.close()


def stats_loop(args, t0: float, rec: Recorder, stop: threading.Event):
    prefix = PROJECT + "-"
    while not stop.is_set():
        started = time.monotonic()
        try:
            out = subprocess.run(["docker", "stats", "--no-stream", "--format", "{{json .}}"],
                                 capture_output=True, text=True, timeout=60).stdout
        except subprocess.TimeoutExpired:
            out = ""
        t = round(time.time() - t0, 2)
        for line in out.splitlines():
            try:
                s = json.loads(line)
            except json.JSONDecodeError:
                continue
            name = s.get("Name", "")
            if not name.startswith(prefix):
                continue
            service = name[len(prefix):].rsplit("-", 1)[0]
            used, _, limit = s.get("MemUsage", "").partition(" / ")
            net_in, _, net_out = s.get("NetIO", "").partition(" / ")
            rec.write("docker_stats", {
                "t": t, "service": service,
                "mem_bytes": parse_size(used), "mem_limit_bytes": parse_size(limit),
                "cpu_pct": float(s.get("CPUPerc", "0").rstrip("%") or 0),
                "pids": int(s.get("PIDs", 0) or 0),
                "net_in": parse_size(net_in), "net_out": parse_size(net_out),
            })
        stop.wait(max(0.0, args.stats_interval - (time.monotonic() - started)))


def progress_loop(args, t0: float, rec: Recorder, stop: threading.Event, counters: dict):
    while not stop.wait(60):
        elapsed = time.time() - t0
        parts = []
        for svc in TARGETS:
            s = rec.latest.get(("scrapes", svc), {})
            d = rec.latest.get(("docker_stats", svc), {})
            mem = d.get("mem_bytes")
            g = s.get("goroutines")
            parts.append(f"{svc}: {'ok' if s.get('ok') else 'FAIL'} "
                         f"mem={mem / MIB:.1f}MiB" if isinstance(mem, float) else f"{svc}: n/a")
            if g not in (None, ""):
                parts[-1] += f" gor={int(float(g))}"
        log(f"{elapsed / 60:4.1f}/{args.duration / 60:.0f} min | " + " | ".join(parts))


# --------------------------------------------------------------------------
# analysis
# --------------------------------------------------------------------------

def linreg(xs: list[float], ys: list[float]):
    n = len(xs)
    if n < 3:
        return 0.0, 0.0
    mx, my = sum(xs) / n, sum(ys) / n
    sxx = sum((x - mx) ** 2 for x in xs)
    syy = sum((y - my) ** 2 for y in ys)
    if sxx == 0:
        return 0.0, 0.0
    sxy = sum((x - mx) * (y - my) for x, y in zip(xs, ys))
    slope = sxy / sxx
    r2 = (sxy * sxy) / (sxx * syy) if syy else 0.0
    return slope, r2


def read_csv(path: Path) -> list[dict]:
    if not path.exists():
        return []
    with open(path, newline="", encoding="utf-8") as f:
        return list(csv.DictReader(f))


def series_of(rows, service, key):
    pts = []
    for r in rows:
        if r["service"] != service or r.get(key) in (None, "", "nan"):
            continue
        try:
            pts.append((float(r["t"]), float(r[key])))
        except ValueError:
            pass
    return pts


# kind: "bytes" (memory) or "count" (handles / series)
RESOURCES = [
    ("docker_stats", "mem_bytes", "Container memory", "bytes"),
    ("scrapes", "rss_bytes", "Process RSS", "bytes"),
    ("scrapes", "heap_inuse_bytes", "Go heap in use", "bytes"),
    ("scrapes", "goroutines", "Goroutines", "count"),
    ("scrapes", "go_threads", "OS threads", "count"),
    ("docker_stats", "pids", "PIDs (processes + threads)", "count"),
    ("scrapes", "open_fds", "Open file descriptors", "count"),
    ("scrapes", "series", "Series per scrape", "count"),
]


def judge(pts, kind, warmup, duration):
    """Trend of one resource after warm-up, and whether it looks like a leak."""
    body = [(t, v) for t, v in pts if t >= warmup]
    if len(body) < 6:
        return None
    xs = [t / 3600 for t, _ in body]
    ys = [v for _, v in body]
    slope, r2 = linreg(xs, ys)
    k = max(3, len(ys) // 10)
    first, last = statistics.median(ys[:k]), statistics.median(ys[-k:])
    window_h = (body[-1][0] - body[0][0]) / 3600 or 1e-9
    trend = slope * window_h  # growth along the fitted line over the window
    median = statistics.median(ys)
    if kind == "bytes":
        limit = max(2 * MIB, 0.05 * median)
    else:
        limit = max(3.0, 0.05 * median)
    # A leak is steady growth: the fitted trend exceeds the noise limit, the
    # line explains most of the variance (not a GC sawtooth), and there is
    # enough post-warm-up data for that to mean something.
    suspect = trend > limit and r2 >= 0.6 and last > first and window_h >= 10 / 60
    return {
        "first": first, "last": last, "min": min(ys), "max": max(ys), "median": median,
        "slope_h": slope, "r2": r2, "trend": trend, "limit": limit, "suspect": suspect,
        "per_day": slope * 24, "points": pts, "warm": body,
    }


def fmt(v, kind):
    if v is None or v == "":
        return "-"
    if kind == "bytes":
        return f"{v / MIB:.1f} MiB"
    return f"{v:.0f}" if abs(v) >= 10 or float(v).is_integer() else f"{v:.1f}"


def fmt_rate(v, kind):
    sign = "+" if v >= 0 else "-"
    return f"{sign}{fmt(abs(v), kind)}/h"


def analyze(out: Path, meta: dict) -> dict:
    scrapes = read_csv(out / "scrapes.csv")
    stats = read_csv(out / "docker_stats.csv")
    redirects = read_csv(out / "redirects.csv")
    states = meta.get("containers", {})
    duration = float(meta.get("elapsed_s") or meta.get("duration_s") or 0)
    warmup = float(meta["warmup_s"])

    result = {"services": {}, "meta": meta}
    for svc in TARGETS:
        s_rows = [r for r in scrapes if r["service"] == svc]
        ok = [r for r in s_rows if r["ok"] == "1"]
        lat = sorted(float(r["latency_ms"]) for r in ok) or [0.0]
        errors: dict[str, int] = {}
        for r in s_rows:
            if r["ok"] != "1":
                key = r["error"].split(":")[0] or f"HTTP {r['status']}"
                errors[key] = errors.get(key, 0) + 1
        red = [r for r in redirects if r["service"] == svc]
        info = {
            "scrapes": len(s_rows), "ok": len(ok),
            "fail_pct": 100 * (len(s_rows) - len(ok)) / max(1, len(s_rows)),
            "lat_p50": lat[len(lat) // 2], "lat_p95": lat[int(len(lat) * 0.95) - 1 if len(lat) > 1 else 0],
            "lat_max": lat[-1], "errors": errors,
            "redirect_ok": sum(r["ok"] == "1" for r in red), "redirect_total": len(red),
            "redirect_example": red[-1]["status"] + " -> " + red[-1]["location"] if red else "-",
            "container": states.get(svc, {}),
            "cpu_avg": statistics.mean([float(r["cpu_pct"]) for r in stats if r["service"] == svc] or [0]),
            "resources": {},
        }
        for src, key, label, kind in RESOURCES:
            pts = series_of(stats if src == "docker_stats" else scrapes, svc, key)
            j = judge(pts, kind, warmup, duration)
            if j:
                info["resources"][key] = {"label": label, "kind": kind, **j}

        problems = []
        c = info["container"]
        if c and (c.get("Status") != "running" or c.get("OOMKilled")):
            problems.append(f"container {c.get('Status')} (exit {c.get('ExitCode')}, OOMKilled={c.get('OOMKilled')})")
        if info["fail_pct"] > 1:
            problems.append(f"{info['fail_pct']:.1f}% scrapes failed")
        if info["redirect_total"] and info["redirect_ok"] < info["redirect_total"]:
            problems.append("/ does not redirect to /metrics")
        leaks = [r["label"] for r in info["resources"].values() if r["suspect"]]
        info["problems"], info["leaks"] = problems, leaks
        info["verdict"] = "FAIL" if problems and c.get("Status") != "running" else (
            "LEAK?" if leaks else ("WARN" if problems else "OK"))
        result["services"][svc] = info
    return result


# --------------------------------------------------------------------------
# reports
# --------------------------------------------------------------------------

def write_markdown(out: Path, res: dict) -> str:
    meta = res["meta"]
    lines = [f"# Soak test {meta['started']}", "",
             f"Duration {float(meta.get('elapsed_s', 0)) / 60:.1f} min, warm-up {meta['warmup_s'] / 60:.0f} min "
             f"excluded from trends, scrape every {meta['scrape_interval']}s (timeout {meta['scrape_timeout']}s), "
             f"docker stats every {meta['stats_interval']}s.", "",
             "| Exporter | Verdict | Scrapes ok | p95 latency | `/` redirect | Container memory (start → end) | Trend |",
             "|---|---|---|---|---|---|---|"]
    for svc, i in res["services"].items():
        m = i["resources"].get("mem_bytes")
        mem = f"{fmt(m['first'], 'bytes')} → {fmt(m['last'], 'bytes')}" if m else "-"
        trend = fmt_rate(m["slope_h"], "bytes") + f" (R² {m['r2']:.2f})" if m else "-"
        lines.append(f"| {svc} | **{i['verdict']}** | {i['ok']}/{i['scrapes']} | {i['lat_p95']:.0f} ms | "
                     f"{i['redirect_ok']}/{i['redirect_total']} | {mem} | {trend} |")
    for svc, i in res["services"].items():
        lines += ["", f"## {svc} — {i['verdict']}", ""]
        for p in i["problems"]:
            lines.append(f"- ⚠ {p}")
        for l in i["leaks"]:
            lines.append(f"- ⚠ {l} keeps growing after warm-up")
        if i["errors"]:
            lines.append("- scrape errors: " + ", ".join(f"{k} ×{v}" for k, v in i["errors"].items()))
        lines.append(f"- latency p50/p95/max: {i['lat_p50']:.0f} / {i['lat_p95']:.0f} / {i['lat_max']:.0f} ms, "
                     f"avg CPU {i['cpu_avg']:.2f}%")
        lines += ["", "| Resource | start | end | min | max | trend | R² | leak? |", "|---|---|---|---|---|---|---|---|"]
        for r in i["resources"].values():
            k = r["kind"]
            lines.append(f"| {r['label']} | {fmt(r['first'], k)} | {fmt(r['last'], k)} | {fmt(r['min'], k)} | "
                         f"{fmt(r['max'], k)} | {fmt_rate(r['slope_h'], k)} | {r['r2']:.2f} | "
                         f"{'⚠ yes' if r['suspect'] else 'no'} |")
    text = "\n".join(lines) + "\n"
    (out / "report.md").write_text(text, encoding="utf-8")
    return text


def svg_chart(r: dict, warmup: float, width=320, height=120) -> str:
    pts = r["points"]
    if not pts:
        return ""
    k = r["kind"]
    scale = MIB if k == "bytes" else 1
    xs = [t for t, _ in pts]
    ys = [v / scale for _, v in pts]
    x0, x1 = min(xs), max(xs) or 1
    lo, hi = min(ys), max(ys)
    pad = (hi - lo) * 0.15 or max(1.0, abs(hi) * 0.05)
    lo, hi = lo - pad, hi + pad
    L, R, T, B = 44, 8, 8, 20

    def X(t):
        return L + (t - x0) / ((x1 - x0) or 1) * (width - L - R)

    def Y(v):
        return T + (hi - v) / ((hi - lo) or 1) * (height - T - B)

    step = max(1, len(pts) // 400)
    poly = " ".join(f"{X(t):.1f},{Y(v / scale):.1f}" for t, v in pts[::step])
    warm = r["warm"]
    fit = ""
    if warm:
        wx = [t for t, _ in warm]
        mid_t, mid_v = statistics.mean(wx), statistics.mean(v for _, v in warm) / scale
        sl = r["slope_h"] / 3600 / scale
        a, b = wx[0], wx[-1]
        fit = (f'<line x1="{X(a):.1f}" y1="{Y(mid_v + sl * (a - mid_t)):.1f}" x2="{X(b):.1f}" '
               f'y2="{Y(mid_v + sl * (b - mid_t)):.1f}" class="fit"/>')
    unit = "MiB" if k == "bytes" else ""
    wx_ = X(min(warmup, x1))
    return (f'<svg viewBox="0 0 {width} {height}" role="img" aria-label="{html.escape(r["label"])}">'
            f'<rect x="{L}" y="{T}" width="{max(0, wx_ - L):.1f}" height="{height - T - B}" class="warm"/>'
            f'<line x1="{L}" y1="{height - B}" x2="{width - R}" y2="{height - B}" class="axis"/>'
            f'<text x="{L - 4}" y="{T + 8}" class="tick" text-anchor="end">{hi:.1f}{unit}</text>'
            f'<text x="{L - 4}" y="{height - B}" class="tick" text-anchor="end">{lo:.1f}{unit}</text>'
            f'<text x="{L}" y="{height - 6}" class="tick">0</text>'
            f'<text x="{width - R}" y="{height - 6}" class="tick" text-anchor="end">{x1 / 60:.0f} min</text>'
            f'<polyline points="{poly}" class="line{" bad" if r["suspect"] else ""}"/>{fit}</svg>')


def write_html(out: Path, res: dict) -> None:
    meta = res["meta"]
    warmup = float(meta["warmup_s"])
    badge = {"OK": "ok", "WARN": "warn", "LEAK?": "bad", "FAIL": "bad"}
    rows, cards = [], []
    for svc, i in res["services"].items():
        m = i["resources"].get("mem_bytes", {})
        rows.append(
            f"<tr><td>{svc}</td><td><span class='b {badge[i['verdict']]}'>{i['verdict']}</span></td>"
            f"<td>{i['ok']}/{i['scrapes']}</td><td>{i['lat_p95']:.0f} ms</td>"
            f"<td>{i['redirect_ok']}/{i['redirect_total']}</td>"
            f"<td>{fmt(m.get('first'), 'bytes')} → {fmt(m.get('last'), 'bytes')}</td>"
            f"<td>{fmt_rate(m['slope_h'], 'bytes') if m else '-'}</td></tr>")
        charts = []
        for key in ("mem_bytes", "rss_bytes", "heap_inuse_bytes", "goroutines", "pids", "open_fds", "series"):
            r = i["resources"].get(key)
            if not r:
                continue
            k = r["kind"]
            charts.append(
                f"<figure><figcaption>{html.escape(r['label'])}<span>{fmt(r['first'], k)} → {fmt(r['last'], k)}, "
                f"{fmt_rate(r['slope_h'], k)}, R² {r['r2']:.2f}</span></figcaption>{svg_chart(r, warmup)}</figure>")
        notes = "".join(f"<li>{html.escape(p)}</li>" for p in i["problems"] + [f"{l} keeps growing" for l in i["leaks"]])
        errs = ", ".join(f"{html.escape(k)} ×{v}" for k, v in i["errors"].items())
        cards.append(
            f"<section><h2>{svc} <span class='b {badge[i['verdict']]}'>{i['verdict']}</span></h2>"
            f"<p class='sub'>p50/p95/max {i['lat_p50']:.0f}/{i['lat_p95']:.0f}/{i['lat_max']:.0f} ms · "
            f"avg CPU {i['cpu_avg']:.2f}% · GET / → {html.escape(i['redirect_example'])}"
            f"{' · errors: ' + errs if errs else ''}</p>"
            f"{'<ul>' + notes + '</ul>' if notes else ''}<div class='grid'>{''.join(charts)}</div></section>")
    doc = f"""<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>Exporter soak test</title>
<style>
:root{{--bg:#fafaf9;--fg:#1c1917;--muted:#78716c;--card:#fff;--line:#e7e5e4;--accent:#2563eb;--bad:#dc2626;--warn:#d97706;--ok:#16a34a}}
@media (prefers-color-scheme:dark){{:root{{--bg:#0c0a09;--fg:#e7e5e4;--muted:#a8a29e;--card:#1c1917;--line:#292524;--accent:#60a5fa;--bad:#f87171;--warn:#fbbf24;--ok:#4ade80}}}}
body{{margin:0;padding:24px 16px;background:var(--bg);color:var(--fg);font:14px/1.5 system-ui,sans-serif}}
main{{max-width:1100px;margin:auto}} h1{{font-size:22px;margin:0 0 4px}} h2{{font-size:17px;margin:0 0 4px}}
.sub{{color:var(--muted);margin:0 0 12px}} table{{width:100%;border-collapse:collapse;margin:16px 0 24px;background:var(--card)}}
td,th{{padding:6px 10px;border-bottom:1px solid var(--line);text-align:left;white-space:nowrap}}
.tablewrap{{overflow-x:auto}} section{{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:16px;margin:0 0 16px}}
.grid{{display:grid;grid-template-columns:repeat(auto-fill,minmax(300px,1fr));gap:12px}}
figure{{margin:0}} figcaption{{font-weight:600;font-size:13px}} figcaption span{{display:block;font-weight:400;color:var(--muted)}}
svg{{width:100%;height:auto}} .line{{fill:none;stroke:var(--accent);stroke-width:1.5}} .line.bad{{stroke:var(--bad)}}
.fit{{stroke:var(--fg);stroke-dasharray:4 3;opacity:.6}} .axis{{stroke:var(--line)}} .warm{{fill:var(--line);opacity:.5}}
.tick{{font-size:9px;fill:var(--muted)}} .b{{font-size:11px;font-weight:700;padding:2px 7px;border-radius:99px;color:#fff;vertical-align:middle}}
.b.ok{{background:var(--ok)}} .b.warn{{background:var(--warn)}} .b.bad{{background:var(--bad)}}
</style></head><body><main>
<h1>Exporter soak test</h1>
<p class="sub">{html.escape(meta['started'])} · {float(meta.get('elapsed_s', 0)) / 60:.1f} min · scrape every {meta['scrape_interval']}s ·
shaded area = {warmup / 60:.0f} min warm-up (excluded from trends) · dashed line = fitted trend</p>
<div class="tablewrap"><table><tr><th>Exporter</th><th>Verdict</th><th>Scrapes ok</th><th>p95</th><th>/ redirect</th>
<th>Container memory</th><th>Trend</th></tr>{''.join(rows)}</table></div>
{''.join(cards)}</main></body></html>"""
    (out / "report.html").write_text(doc, encoding="utf-8")


def container_states() -> dict:
    states = {}
    ids = compose("ps", "-a", "-q", check=False, capture=True).stdout.split()
    if not ids:
        return states
    out = subprocess.run(["docker", "inspect", *ids], capture_output=True, text=True).stdout
    for c in json.loads(out or "[]"):
        svc = c["Config"]["Labels"].get("com.docker.compose.service", c["Name"])
        st = c["State"]
        states[svc] = {"Status": st.get("Status"), "ExitCode": st.get("ExitCode"),
                       "OOMKilled": st.get("OOMKilled"), "RestartCount": c.get("RestartCount", 0)}
    return states


# --------------------------------------------------------------------------
# main
# --------------------------------------------------------------------------

def wait_ready(timeout: float) -> None:
    deadline = time.time() + timeout
    pending = dict(TARGETS)
    while pending and time.time() < deadline:
        for svc, port in list(pending.items()):
            try:
                status, _, _, c = get(port, "/metrics", 5)
                c.close()
                if status == 200:
                    log(f"{svc} is up on :{port}")
                    del pending[svc]
            except Exception:
                pass
        time.sleep(2)
    if pending:
        log(f"WARNING: not answering after {timeout:.0f}s: {', '.join(pending)} (continuing; failures are recorded)")


def run(args) -> Path:
    out = ROOT / "soak" / "results" / datetime.now().strftime("%Y%m%d-%H%M%S")
    out.mkdir(parents=True)
    log(f"results -> {out.relative_to(ROOT)}")

    log("starting stack" + (" (building images)" if args.build else ""))
    compose("up", "-d", *(["--build"] if args.build else []), "--force-recreate", "--remove-orphans")
    wait_ready(120)

    meta = {"started": datetime.now().isoformat(timespec="seconds"), "duration_s": args.duration,
            "warmup_s": args.warmup, "scrape_interval": args.scrape_interval,
            "scrape_timeout": args.scrape_timeout, "stats_interval": args.stats_interval}
    rec = Recorder(out)
    stop = threading.Event()
    t0 = time.time()
    threads = [threading.Thread(target=scrape_loop, args=(svc, port, args, t0, rec, stop), daemon=True)
               for svc, port in TARGETS.items()]
    threads.append(threading.Thread(target=stats_loop, args=(args, t0, rec, stop), daemon=True))
    threads.append(threading.Thread(target=progress_loop, args=(args, t0, rec, stop, {}), daemon=True))
    for t in threads:
        t.start()

    interrupted = False

    def on_sigint(*_):
        nonlocal interrupted
        interrupted = True
        stop.set()

    signal.signal(signal.SIGINT, on_sigint)
    log(f"soaking for {args.duration / 60:.1f} min (Ctrl+C to stop early and still get a report)")
    stop.wait(args.duration)
    stop.set()
    for t in threads:
        t.join(timeout=args.scrape_timeout + 5)
    rec.close()

    meta["elapsed_s"] = time.time() - t0
    meta["interrupted"] = interrupted
    meta["containers"] = container_states()
    (out / "meta.json").write_text(json.dumps(meta, indent=2), encoding="utf-8")
    logs = compose("logs", "--no-color", "--timestamps", check=False, capture=True).stdout
    (out / "logs.txt").write_text(logs, encoding="utf-8")

    if not args.keep_running:
        log("stopping stack")
        compose("down", "--remove-orphans", check=False, capture=True)
    return out


def report(out: Path) -> int:
    meta = json.loads((out / "meta.json").read_text(encoding="utf-8"))
    res = analyze(out, meta)
    text = write_markdown(out, res)
    write_html(out, res)
    print()
    print(text)
    log(f"report: {out / 'report.html'}")
    return 1 if any(i["verdict"] in ("FAIL", "LEAK?") for i in res["services"].values()) else 0


def main() -> int:
    for stream in (sys.stdout, sys.stderr):  # Windows consoles default to cp1252
        try:
            stream.reconfigure(encoding="utf-8", errors="replace")
        except AttributeError:
            pass
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--duration", type=parse_duration, default="30m", help="how long to soak (default 30m)")
    p.add_argument("--warmup", type=parse_duration, default=None,
                   help="initial period excluded from trend analysis (default: 5m, or 1/4 of short runs)")
    p.add_argument("--scrape-interval", type=float, default=5, help="seconds between scrapes (default 5)")
    p.add_argument("--scrape-timeout", type=float, default=10, help="scrape timeout in seconds (default 10, like Prometheus)")
    p.add_argument("--stats-interval", type=float, default=10, help="seconds between docker stats samples (default 10)")
    p.add_argument("--no-build", dest="build", action="store_false", help="reuse already built images")
    p.add_argument("--keep-running", action="store_true", help="leave the stack running afterwards")
    p.add_argument("--analyze", type=Path, help="only (re)analyse an existing results directory")
    args = p.parse_args()
    if args.warmup is None:
        args.warmup = min(300.0, args.duration / 4)

    if args.analyze:
        return report(args.analyze.resolve())
    return report(run(args))


if __name__ == "__main__":
    sys.exit(main())
