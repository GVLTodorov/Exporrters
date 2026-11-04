# binance exporter

Prometheus exporter for cryptocurrency spot prices from the public Binance API, no API key needed.

**Image** `ghcr.io/gvltodorov/binanceexporrter:latest` · **Port** `8080` · [All exporters](../README.md)

## Run

```yaml
services:
  binance-exporter:
    image: ghcr.io/gvltodorov/binanceexporrter:latest
    restart: unless-stopped
    ports:
      - 8088:8080
    environment:
      - SYMBOLS=BTCUSDT,ETHUSDT
```

## Configuration

| Variable | Default | Description |
|---|---|---|
| `SYMBOLS` | `BTCUSDT,ETHUSDT,BNBUSDT` | Comma-separated Binance trading pairs |
| `BINANCE_BASE_URL` | `https://api.binance.com` | Another Binance-compatible API (mirror or proxy) |

## Metrics

| Metric | Labels | Description |
|---|---|---|
| `binance_crypto_price` | `symbol` | Last price of the trading pair |

## Notes

- Prices are fetched on every scrape, in one request for all symbols
  (`/api/v3/ticker/price`).
- If Binance can't be reached or rejects the request (for example an unknown
  symbol), the scrape fails with HTTP 500 and Prometheus marks the target
  down.
- Binance blocks some regions (HTTP 451). There, set `BINANCE_BASE_URL` to a
  mirror you are allowed to use.

## Development

```bash
go test ./... -race -skip Leak    # unit tests
go test ./... -run Leak -count=3  # leak tests
```
