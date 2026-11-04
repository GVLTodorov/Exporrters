# weather exporter

Prometheus exporter for Open-Meteo current weather, 48-hour and 7-day forecasts, and air quality for one location.

**Image** `ghcr.io/gvltodorov/exporrters/weather:latest` · **Port** `8080` · [All exporters](../README.md)

## Run

```yaml
services:
  weather-exporter:
    image: ghcr.io/gvltodorov/exporrters/weather:latest
    restart: unless-stopped
    ports:
      - 9080:8080
    environment:
      - LATITUDE=42.6975
      - LONGITUDE=23.3241
      - TIMEZONE=Europe/Sofia
```

## Configuration

| Variable | Default | Description |
|---|---|---|
| `LATITUDE` | `42.6975` | Latitude in decimal degrees |
| `LONGITUDE` | `23.3241` | Longitude in decimal degrees |
| `TIMEZONE` | `Europe/Sofia` | IANA timezone, so forecast times line up with local time |
| `FETCH_INTERVAL` | `10m` | How often to call Open-Meteo (Go duration). Invalid values fall back to `10m` |

## Metrics

| Prefix | Source | Labels |
|---|---|---|
| `t_<field>` | Forecast, current | |
| `t_forecast_<field>` | Forecast, hourly | `hour_offset` `0`–`47` |
| `t_daily_<field>` | Forecast, daily | `day_offset` `0`–`6` |
| `a_<field>` | Air quality, current | |
| `a_forecast_<field>` | Air quality, hourly | `hour_offset` `0`–`47` |

Time fields (`time`, `sunrise`, `sunset`) are also exported as `_seconds`
gauges in Unix time. Every metric, with units, the weather-code table, AQI
scales and a Grafana forecast recipe: [metrics.md](metrics.md).

## Notes

- About 4,000 series per scrape, with a fixed label set.
- Fields Open-Meteo doesn't return for your location are left out, not set
  to 0.
- Timezone data is built into the binary, so `TIMEZONE` works on any base
  image.

## Development

```bash
go test ./... -race -skip Leak    # unit tests
go test ./... -run Leak -count=3  # leak tests
```
