# mikrotik exporter

Prometheus exporter for one MikroTik RouterOS device: system, interface, DHCP, connection and temperature stats over the RouterOS API.

**Image** `ghcr.io/gvltodorov/mikrotikexporrter:latest` · **Port** `8080` · [All exporters](../README.md)

## Run

```yaml
services:
  mikrotik-exporter:
    image: ghcr.io/gvltodorov/mikrotikexporrter:latest
    restart: unless-stopped
    ports:
      - 9436:8080
    environment:
      - MIKROTIK_ADDRESS=192.168.88.1
      - MIKROTIK_USER=prometheus
      - MIKROTIK_PASSWORD=change-me
```

Create a read-only API user on the router:

```
/user group add name=prometheus-ro policy=read,api,rest-api
/user add name=prometheus password=change-me group=prometheus-ro
```

## Configuration

| Variable | Default | Description |
|---|---|---|
| `MIKROTIK_ADDRESS` | *(required)* | Router hostname or IP, no port |
| `MIKROTIK_PASSWORD` | *(required)* | Password for `MIKROTIK_USER` |
| `MIKROTIK_USER` | `prometheus` | Needs read access to `system`, `interface`, `ip dhcp-server`, `ip firewall` |
| `MIKROTIK_API_PORT` | `8728` (`8729` with TLS) | RouterOS API port |
| `MIKROTIK_USE_TLS` | `false` | Use the encrypted `api-ssl` service |
| `MIKROTIK_INSECURE_SKIP_VERIFY` | `true` | Skip TLS certificate checks (RouterOS ships a self-signed cert) |
| `MIKROTIK_TIMEOUT` | `10s` | Connect and per-request timeout |
| `FETCH_INTERVAL` | `15s` | How often to poll the router |
| `LISTEN_PORT` | `8080` | Port for `/metrics` |

## Metrics

| Metric | Labels | Description |
|---|---|---|
| `mikrotik_up` | | 1 if the last poll fully succeeded |
| `mikrotik_cpu_load_percent` | | CPU load |
| `mikrotik_memory_free_bytes`, `mikrotik_memory_total_bytes` | | RAM |
| `mikrotik_storage_free_bytes`, `mikrotik_storage_total_bytes` | | Disk |
| `mikrotik_uptime_seconds` | | Uptime |
| `mikrotik_board_info` | `board_name`, `version` | Always 1 |
| `mikrotik_interface_{rx,tx}_{bytes,packets,errors,drops}_total` | `interface` | RouterOS interface counters |
| `mikrotik_interface_up` | `interface` | 1 if running |
| `mikrotik_interface_enabled` | `interface` | 1 if not disabled |
| `mikrotik_dhcp_leases_active` | | Bound DHCP leases |
| `mikrotik_connections_total` | `protocol` (`tcp`/`udp`) | Active firewall connections |
| `mikrotik_health_temperature_celsius` | `sensor` | Temperature sensors (RouterOS v7) |

## Notes

- Uses the classic binary API (`api`, enabled by default), not REST, which
  needs `www`/`www-ssl`.
- Plain `api` sends credentials unencrypted on the LAN. For encryption, run
  `/ip/service/enable api-ssl` and set `MIKROTIK_USE_TLS=true`.
- Interfaces are re-read every poll. A removed interface drops out of
  `/metrics` instead of leaving a stale series.
- Background and design goals: [requirements.md](requirements.md).

## Development

```bash
go test ./... -race -skip Leak    # unit tests
go test ./... -run Leak -count=3  # leak tests
```
