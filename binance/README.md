# BinanceExporrter

A Prometheus exporter for cryptocurrency spot prices from the public
Binance API (`/api/v3/ticker/price`). No API key needed.

Prices are fetched on every scrape of `/metrics`, in a single request for
all symbols. Opening `/` redirects to `/metrics`.

## Metrics

| Metric | Labels | Description |
|---|---|---|
| `binance_crypto_price` | `symbol` | Last price of the trading pair |

If Binance can't be reached or rejects the request (for example an unknown
symbol), the scrape fails with HTTP 500 and Prometheus marks the target
down.

## Configuration

| Variable | Default | Description |
|---|---|---|
| `SYMBOLS` | `BTCUSDT,ETHUSDT,BNBUSDT` | Comma-separated Binance trading pairs |
| `BINANCE_BASE_URL` | `https://api.binance.com` | Another Binance-compatible API (mirror, proxy, or the soak test fake) |

Binance blocks some regions (HTTP 451). There, use `BINANCE_BASE_URL` to
point at a mirror you are allowed to use.

## Docker

```yaml
services:
  binance-exporter:
    image: ghcr.io/gvltodorov/exporrters/binance:latest
    container_name: binance-exporter
    ports:
      - 8088:8080
    environment:
      - SYMBOLS=BTCUSDT,ETHUSDT
```
