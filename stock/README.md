# stock exporter

Prometheus exporter for stock prices (previous trading day's close) from the Polygon.io API.

**Image** `ghcr.io/gvltodorov/exporrters/stock:latest` · **Port** `8080` · [All exporters](../README.md)

## Run

```yaml
services:
  stock-exporter:
    image: ghcr.io/gvltodorov/exporrters/stock:latest
    restart: unless-stopped
    ports:
      - 8087:8080
    environment:
      - POLYGON_API_KEY=change-me
      - SYMBOLS=QCOM,NVDA,INTC
```

## Configuration

| Variable | Default | Description |
|---|---|---|
| `POLYGON_API_KEY` | *(required)* | Polygon.io API key |
| `SYMBOLS` | *(required)* | Comma-separated ticker symbols |
| `POLYGON_BASE_URL` | `https://api.polygon.io` | Another Polygon-compatible API |

## Metrics

| Metric | Labels | Description |
|---|---|---|
| `stock_price` | `symbol` | Previous day's close (`/v2/aggs/ticker/<symbol>/prev`) |

## Notes

- Prices are cached for 2 minutes, and requests are capped at 5 per minute
  to stay within the free tier.
- When Polygon rate-limits a request, the last cached price is served.

## Development

```bash
go test ./... -race -skip Leak    # unit tests
go test ./... -run Leak -count=3  # leak tests
```
