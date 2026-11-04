# Exporrters

Prometheus exporters, one per folder. Each one is released as its own image.
**Pick an exporter below for its setup, configuration and metrics.**

| Exporter | What it exports | Image |
|---|---|---|
| [binance](binance/#readme) | Crypto spot prices from the Binance API | `ghcr.io/gvltodorov/exporrters/binance` |
| [mikrotik](mikrotik/#readme) | MikroTik RouterOS system, interface and connection stats | `ghcr.io/gvltodorov/exporrters/mikrotik` |
| [ping](ping/#readme) | ICMP loss and latency (drop-in for `czerwonk/ping_exporter`) | `ghcr.io/gvltodorov/exporrters/ping` |
| [stock](stock/#readme) | Stock prices from Polygon.io | `ghcr.io/gvltodorov/exporrters/stock` |
| [weather](weather/#readme) | Open-Meteo weather, forecast and air quality | `ghcr.io/gvltodorov/exporrters/weather` |
| [xiaomi](xiaomi/#readme) | Xiaomi air purifier / humidifier sensors | `ghcr.io/gvltodorov/exporrters/xiaomi` |

Every exporter serves `/metrics`; opening `/` redirects there. Tags:
`latest`, `X.Y` and `X.Y.Z`.

Building, testing and releasing: [DEVELOPMENT.md](DEVELOPMENT.md).
