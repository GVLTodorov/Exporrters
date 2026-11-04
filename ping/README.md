# ping exporter

Prometheus ICMP ping exporter, a drop-in replacement for czerwonk/ping_exporter with the same config file, port and metrics.

**Image** `ghcr.io/gvltodorov/pingexporrter:latest` · **Port** `9427` · [All exporters](../README.md)

## Run

```yaml
services:
  ping-exporter:
    image: ghcr.io/gvltodorov/pingexporrter:latest
    restart: unless-stopped
    ports:
      - 9427:9427
    volumes:
      - ./config.yml:/config/config.yml:ro
```

`config.yml` (only `targets` is required; the values shown are the defaults):

```yaml
targets:
  - 1.1.1.1          # IP literal
  - example.com      # hostname, re-resolved every dns.refresh

dns:
  refresh: 5m
  nameserver: 1.1.1.1

ping:
  interval: 2s       # per target
  timeout: 3s        # a probe slower than this counts as lost
  history-size: 50   # rolling window (probes) for the stats
  size: 56           # ICMP payload bytes

options:
  disableIPv6: false # true: don't ping AAAA addresses
```

## Configuration

| Variable | Default | Description |
|---|---|---|
| `CONFIG_FILE` | `/config/config.yml` | Path to the YAML config |
| `LISTEN_ADDR` | `:9427` | Address for `/metrics` |

## Metrics

| Metric | Labels | Description |
|---|---|---|
| `ping_loss_ratio` | `ip`, `ip_version`, `target` | Share of probes lost in the window, 0–1 |
| `ping_rtt_best_seconds` | `ip`, `ip_version`, `target` | Minimum RTT in the window |
| `ping_rtt_mean_seconds` | `ip`, `ip_version`, `target` | Mean RTT in the window |
| `ping_rtt_worst_seconds` | `ip`, `ip_version`, `target` | Maximum RTT in the window |
| `ping_rtt_std_deviation_seconds` | `ip`, `ip_version`, `target` | RTT standard deviation |
| `ping_up` | `version` | Always 1 while the process runs |

`target` is the value from `targets:`, `ip` is the address pinged and
`ip_version` is `4` or `6`.

## Notes

- Runs as a non-root user. The binary has `cap_net_raw` set in the image, so
  no `cap_add` is needed.
- One shared raw socket per IP family, not one per target.
- When a hostname stops resolving to an IP, that IP's series disappear.
- Background and acceptance criteria:
  [PING_EXPORTER_REQUIREMENTS.md](PING_EXPORTER_REQUIREMENTS.md).

## Development

```bash
go test ./... -race -skip Leak    # unit tests
go test ./... -run Leak -count=3  # leak tests
```
