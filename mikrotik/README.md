# MikrotikExporrter

Prometheus exporter for a single RouterOS device, over RouterOS's classic binary API (port `8728`/`8729`), via [go-routeros/routeros](https://github.com/go-routeros/routeros). Built to close two gaps neither `nshttpd/mikrotik-exporter` nor `swoga/mikrotik-exporter` covers: active TCP/UDP connection counts and board-name mapping — see [requirements.md](./requirements.md) for the full background.

The REST API was the original plan, but the target router only has the classic `api` service enabled (`www`/`www-ssl` — required for REST — are off by default on most home routers), so this exporter speaks the same binary protocol as the currently-deployed `nshttpd/mikrotik-exporter`.

- One target, one `/metrics` endpoint — no `targets:` list, no YAML config.
- Interface list is read from the router every scrape, not hardcoded.
- Connection and DHCP-lease counts use RouterOS's `print count-only`, never a full table dump.
- `mikrotik_up` reflects the last scrape's success so Grafana/Prometheus can alert on connectivity failures.

## Quick start

```bash
docker run -d \
  --name mikrotikexporter \
  -p 9081:8080 \
  -e MIKROTIK_ADDRESS=192.168.1.1 \
  -e MIKROTIK_USER=prometheus \
  -e MIKROTIK_PASSWORD=changeme \
  ghcr.io/gvltodorov/mikrotikexporrter:latest
```

Then scrape:

```bash
curl http://localhost:9081/metrics
```

## docker-compose

```yaml
  mikrotikexporter:
    image: ghcr.io/gvltodorov/mikrotikexporrter:latest
    container_name: mikrotikexporter
    restart: unless-stopped
    ports:
      - 9081:8080
    environment:
      - MIKROTIK_ADDRESS=192.168.1.1
      - MIKROTIK_USER=prometheus
      - MIKROTIK_PASSWORD=changeme
      - FETCH_INTERVAL=15s
    networks:
      - diagnostic
```

## Configuration

| Variable                        | Default        | Description                                                                 |
|----------------------------------|----------------|-------------------------------------------------------------------------------|
| `MIKROTIK_ADDRESS`               | *(required)*   | Router hostname or IP, no port (e.g. `192.168.1.1`).                         |
| `MIKROTIK_PASSWORD`               | *(required)*   | Password for `MIKROTIK_USER`.                                                |
| `MIKROTIK_USER`                  | `prometheus`   | RouterOS user with (at minimum) read access to `system`, `interface`, `ip dhcp-server`, `ip firewall`. |
| `MIKROTIK_API_PORT`              | `8728` (`8729` if `MIKROTIK_USE_TLS=true`) | RouterOS API port.                              |
| `MIKROTIK_USE_TLS`               | `false`        | Use the encrypted `api-ssl` service instead of plaintext `api`.              |
| `MIKROTIK_INSECURE_SKIP_VERIFY`  | `true`         | Skip TLS certificate verification when `MIKROTIK_USE_TLS=true` (RouterOS ships a self-signed cert by default). |
| `LISTEN_PORT`                    | `8080`         | Port the `/metrics` endpoint listens on.                                     |
| `FETCH_INTERVAL`                 | `15s`          | How often to poll the router (Go duration, e.g. `15s`, `30s`, `1m`).          |
| `MIKROTIK_TIMEOUT`               | `10s`          | Connect + per-request timeout to the router.                                 |

RouterOS setup: the classic `api` service is enabled by default (`/ip/service/print` to check). Create a read-only API user, e.g.:

```
/user group add name=prometheus-ro policy=read,api,rest-api
/user add name=prometheus password=changeme group=prometheus-ro
```

Plaintext `api` (port 8728) sends credentials unencrypted on the LAN — the same tradeoff the currently-deployed `nshttpd` exporter already makes. Set `MIKROTIK_USE_TLS=true` and enable the `api-ssl` service (`/ip/service/enable api-ssl`) for encrypted transport.

## Exposed metrics

| Metric                                | Type  | Labels             | Source                                              |
|----------------------------------------|-------|---------------------|------------------------------------------------------|
| `mikrotik_up`                          | gauge | —                   | 1 if the last scrape fully succeeded, 0 otherwise     |
| `mikrotik_cpu_load_percent`            | gauge | —                   | `/system/resource` → `cpu-load`                       |
| `mikrotik_memory_free_bytes`           | gauge | —                   | `/system/resource` → `free-memory`                    |
| `mikrotik_memory_total_bytes`          | gauge | —                   | `/system/resource` → `total-memory`                   |
| `mikrotik_storage_free_bytes`          | gauge | —                   | `/system/resource` → `free-hdd-space`                 |
| `mikrotik_storage_total_bytes`         | gauge | —                   | `/system/resource` → `total-hdd-space`                |
| `mikrotik_uptime_seconds`              | gauge | —                   | `/system/resource` → `uptime`                         |
| `mikrotik_board_info`                  | gauge | `board_name`, `version` | `/system/resource` → `board-name`, `version` (always 1) |
| `mikrotik_interface_rx_bytes_total`    | gauge | `interface`         | `/interface` → `rx-byte`                              |
| `mikrotik_interface_tx_bytes_total`    | gauge | `interface`         | `/interface` → `tx-byte`                              |
| `mikrotik_interface_rx_packets_total`  | gauge | `interface`         | `/interface` → `rx-packet`                            |
| `mikrotik_interface_tx_packets_total`  | gauge | `interface`         | `/interface` → `tx-packet`                            |
| `mikrotik_interface_rx_errors_total`   | gauge | `interface`         | `/interface` → `rx-error`                             |
| `mikrotik_interface_tx_errors_total`   | gauge | `interface`         | `/interface` → `tx-error`                             |
| `mikrotik_interface_rx_drops_total`    | gauge | `interface`         | `/interface` → `rx-drop`                              |
| `mikrotik_interface_tx_drops_total`    | gauge | `interface`         | `/interface` → `tx-drop`                              |
| `mikrotik_interface_up`                | gauge | `interface`         | `/interface` → `running`                              |
| `mikrotik_interface_enabled`           | gauge | `interface`         | `/interface` → `!disabled`                            |
| `mikrotik_dhcp_leases_active`          | gauge | —                   | `/ip/dhcp-server/lease` count where `status=bound`    |
| `mikrotik_connections_total`           | gauge | `protocol` (`tcp`/`udp`) | `/ip/firewall/connection` count-only, filtered by protocol |

The `*_total`-suffixed interface counters mirror RouterOS's own cumulative counters (they reset on interface reset/router reboot) — same convention as `node_exporter`'s network metrics, so `rate()`/`increase()` in PromQL work as expected. An interface that disappears (e.g. a removed WireGuard peer) drops out of `/metrics` on the next scrape rather than keeping a stale series.

## Development

```bash
go build ./...
go vet ./...
go test ./... -v -cover
```

Tests stub the RouterOS API's `Run(sentence ...string) (*Reply, error)` call, so no real router is required for most of the suite. `leak_test.go` is the exception: it runs `collectOnce`'s real dial/query/Close lifecycle against an in-process fake router (speaking just enough of the wire protocol to log in and ack commands) for 200 cycles, and fails if any connection or goroutine is left behind — `go.uber.org/goleak` backs the whole package via `TestMain`.

## Releases

Every push to `main` bumps a semver tag (patch by default) and publishes `ghcr.io/gvltodorov/mikrotikexporrter:<version>` plus `:latest`. Tests must pass first.
