# BinanceExporrter

```
services:
  binance-exporter:
    image: ghcr.io/gvltodorov/binanceexporrter:beta
    container_name: binance-exporter
    ports:
      - 8086:8080
    environment:
      - SYMBOLS=BTCUSDT
```
