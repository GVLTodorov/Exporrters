# MikrotikExporrter — Requirements

## 1. Purpose

A custom Prometheus exporter for a single RouterOS device, following the same conventions as the existing `WeatherExporrter`, `StockExporrter`, and `XiaomiExporrter` projects (single Go binary, env-var config, Docker image, `client_golang`). Built to close gaps found while evaluating two existing off-the-shelf exporters — neither exposes TCP/UDP connection counts, and each is missing something else the current Grafana dashboard needs.

## 2. Background — why not use an existing exporter

- **`nshttpd/mikrotik-exporter`** (currently deployed on l11): covers interfaces, DHCP, routes, optics, system resource — but has no TCP/UDP connection metric, and the project looks stale upstream.
- **`swoga/mikrotik-exporter`** (evaluated locally 2026-09-01, tested live against the real router): more actively maintained, more detailed interface metrics — but ships **zero DHCP module** and doesn't map board-name either. Also no connection-tracking metric.
- Neither exposes RouterOS's `/ip/firewall/connection` table (active TCP/UDP connection counts) — the original motivating gap for this whole investigation.

## 3. Target device / environment

- Single device, initially: home router at `192.168.1.1`, RouterOS 7.23.3, hAP ax^3
- Auth: dedicated read-only API user (a `prometheus` user already exists on the router for the current exporter — decide whether to reuse it or provision a fresh one, see open questions)
- Deployed as a container on l11 alongside (eventually replacing) the current `mikrotikexporter` service

## 4. Functional requirements — metrics to expose

### 4.1 System resource
*(parity with dashboard "Router Overview" / "Router Summary" panels)*

| Metric | Source | Status |
|---|---|---|
| CPU load % | `/system/resource/print` → `cpu-load` | covered by existing exporters |
| RAM free/total bytes | `/system/resource/print` | covered |
| Storage free/total bytes | `/system/resource/print` | covered |
| Uptime seconds | `/system/resource/print` | covered |
| RouterOS version string | `/system/resource/print` → `version` | covered |
| **Board model/name** (e.g. "hAP ax^3") | `/system/resource/print` → `board-name` | **gap — neither exporter maps this** |

### 4.2 Interfaces
*(parity with "Traffic — Ether1/Bridge/Wireguard" and "WiFi Traffic" panels)*

- Per-interface rx/tx bytes as Prometheus counters, for all interfaces (must be dynamic — read the actual interface list from the router, don't hardcode `ether1`/`bridge`/`wireguard`/`wifi1`/`wifi2`)
- Per-interface rx/tx packets, errors, drops — nice-to-have, both existing exporters already provide this
- Link state (running/enabled) — nice-to-have

### 4.3 DHCP leases
*(gap in `swoga`; present in the currently-deployed `nshttpd` exporter)*

- Active lease count as a single gauge — source `/ip/dhcp-server/lease/print`, filtered to `status=bound`, count only (dashboard just needs the total, currently 33)
- Stretch/decide later: per-lease labeled gauge (mac/ip/hostname) if it turns out to be useful for a future panel — not required for dashboard parity

### 4.4 TCP/UDP connections
*(the original motivating gap — the whole reason this project exists)*

- Active connection count by protocol, e.g. `mikrotik_connections_total{protocol="tcp"}` / `{protocol="udp"}` — source `/ip/firewall/connection/print`
- **Must use a count-only query** (RouterOS supports `?count-only=` / `.proplist` tricks) rather than dumping the full connection table — the conntrack table on a busy router can be large, and polling it every scrape interval must not become a load problem
- v1 scope: totals by protocol only. Breaking down further (by TCP state — established/time-wait/etc.) is a possible v2, not required now
- Needs a real timing test against the hAP ax^3 to confirm scrape cost before this is trusted at a normal 15–30s Prometheus interval

## 5. Non-functional requirements

- **Language/stack**: Go, single `main.go` (or a small package split if it grows) — matches `WeatherExporrter` / `StockExporrter` / `XiaomiExporrter`
- **Config**: environment variables, not a mounted YAML file (matches sibling projects' convention, and this is a single-device exporter so a YAML `targets:` list is unnecessary complexity) — e.g. `MIKROTIK_ADDRESS`, `MIKROTIK_USER`, `MIKROTIK_PASSWORD`, `LISTEN_PORT`
- **RouterOS connection method** — **decided: (b) binary API**, via `go-routeros/routeros/v3` over port 8728. Confirmed 2026-09-01 by scanning the live router: `www`/`www-ssl` (REST) are disabled, only the classic `api` (8728) and `winbox` (8291) services are open — same as what the currently-deployed `nshttpd` exporter already uses. REST (`https://<router>/rest/...`) was the original plan but isn't reachable without first enabling a service on the router.
- Standard single-target `/metrics` endpoint — not swoga's probe-per-target model, since there's only ever one device
- A scrape-success gauge (e.g. `mikrotik_up`) so Prometheus/Grafana can alert on connectivity failures
- Structured logging to stdout
- Dockerfile + GitHub Actions workflow for image build/publish, matching the `.github/` pattern in sibling projects
- README.md documenting env vars and exposed metrics, matching sibling projects' README style

## 6. Deployment

- Runs as a Docker container; once validated, deploys via this repo's `l11/diagnostic/docker-compose.yml` pattern, likely **replacing** the current `mikrotikexporter` (`nshttpd` image) service — the actual swap is a separate decision, out of scope for v1
- All development/testing happens locally (Docker Compose against the real router over LAN) — nothing gets pushed to l10/l11 until this is explicitly promoted

## 7. Out of scope (v1)

- Multi-device support (single router only — no `targets:` list like swoga)
- BGP, OSPF, RADIUS, health/sensor modules (not used on this router)
- Full DHCP lease table export (count only)
- Per-connection conntrack detail (aggregate counts by protocol only)

## 8. Open questions

- ~~REST API vs. binary API client — which do we want long-term?~~ Resolved 2026-09-01: binary API (see §5) — REST isn't reachable without enabling a router service.
- Reuse the existing `prometheus` RouterOS user/credentials, or provision a fresh dedicated one for this project? (Used the existing credentials for the 2026-09-01 validation run against the live router; still open whether the deployed container should keep reusing them or get its own.)
- Keep the `mikrotik_*` metric name prefix (drop-in compatible with the current Grafana dashboard's existing queries) or start a new namespace? Still open — but checked 2026-09-02 against the live "Mikrotik" dashboard's actual JSON (Grafana on l10, uid `000000168`): sharing the `mikrotik_` prefix is **not** enough for drop-in compatibility. Every panel query uses `nshttpd`'s exact names (`mikrotik_system_cpu_load`, `mikrotik_interface_rx_byte`/`tx_byte`, `mikrotik_dhcp_leases_active_count`, ...) plus an `address` label this exporter doesn't emit, and the WiFi panel's legend depends on an interface `comment` label this exporter doesn't currently read.
- Timeline/decision for retiring the `nshttpd` exporter on l11 once this reaches parity?
