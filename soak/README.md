# Soak test

A real-life endurance test. It runs every exporter in Docker against its
real upstream for about 30 minutes, scrapes them all like Prometheus does,
and looks for resources that keep growing.

```bash
python soak/soak.py                  # 30 minutes (default)
python soak/soak.py --duration 5m    # quick check
python soak/soak.py --analyze soak/results/<run>   # re-run the analysis only
```

In VS Code, run the task **soak: run 30 min**.

The script needs Docker and Python 3.9+, with no extra packages. Results go
to `soak/results/<timestamp>/`, which is git-ignored. Open `report.html` for
the charts.

## What runs

| Exporter | Upstream | Changed for the soak |
|---|---|---|
| binance | real Binance public API (no key, no quota concern) | 5 symbols |
| mikrotik | your router (`mikrotik/.env`) | `FETCH_INTERVAL=5s` (prod 15s) |
| ping | internet + LAN targets in [ping-config.yml](ping-config.yml) | hostnames re-resolved every 30s |
| stock | [fake_polygon.py](fake_polygon.py): random prices, 5% 429, 2% 500, 1% slow | production's Polygon quota isn't touched |
| weather | real Open-Meteo | `FETCH_INTERVAL=1m` (prod 10m) |
| xiaomi | your humidifier (`xiaomi/.env`) | none |

The shorter intervals squeeze several hours' worth of refresh cycles into 30
minutes. `restart: "no"` makes sure a crash or OOM kill shows up in the
results instead of being hidden by an automatic restart.

## What is measured

- **Scrapes:** every 5s per exporter, with keep-alive, gzip and a 10s
  timeout, like Prometheus. Recorded per scrape: status, latency, size,
  series count, and the exporter's own `process_resident_memory_bytes`,
  `process_open_fds`, `go_goroutines`, `go_threads` and
  `go_memstats_heap_inuse_bytes`.
- **`docker stats`:** every 10s. Container memory, CPU and PIDs.
- **`GET /`:** every 60s. It must redirect to `/metrics`.
- **At the end:** container state (exit code, OOM kill) and all logs.

## How a leak is flagged

Samples from the warm-up period (the first 5 minutes) are ignored. A
straight line is then fitted to each resource over time. A resource is
flagged **LEAK?** only when all of these hold:

- the fitted growth over the window is more than the noise limit (2 MiB
  or 5% for memory, 3 or 5% for counts)
- the line explains most of the variance (R² ≥ 0.6). Garbage-collection
  ups and downs don't, steady growth does.
- the end value is higher than the start value
- there are at least 10 minutes of data after warm-up

Other verdicts: **FAIL** means the container exited or was OOM-killed.
**WARN** means more than 1% of scrapes failed, or `/` didn't redirect.

A flat 30-minute run is strong evidence against fast leaks. It can't rule
out very slow ones (a few KB per hour). For those, run longer, for example
`--duration 4h`.
