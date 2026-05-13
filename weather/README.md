# WeatherExporrter

Prometheus exporter that pulls **everything** [Open-Meteo](https://open-meteo.com/en/docs) has for a single location and exposes it on `/metrics` — current weather, 48 h hourly forecast, 7-day daily forecast, and current + 48 h hourly air-quality forecast (including pollen and AQI breakdowns where available).

- One scrape, ~4 000 series, bounded label cardinality, no growth over time.
- Only **three** environment variables: `LATITUDE`, `LONGITUDE`, `TIMEZONE`.
- Fields that aren't available at your coordinates are simply absent (no fake zeros).

> Full list of every metric, with units, ranges, weather-code lookup and AQI scales: **[metrics.md](./metrics.md)**.

## Quick start

```bash
docker run -d \
  --name weatherexporter \
  -p 9080:8080 \
  -e LATITUDE=42.6975 \
  -e LONGITUDE=23.3241 \
  -e TIMEZONE=Europe/Sofia \
  ghcr.io/gvltodorov/weatherexporrter:beta
```

Then scrape:

```bash
curl http://localhost:9080/metrics
```

## docker-compose

```yaml
  weatherexporter:
    image: ghcr.io/gvltodorov/weatherexporrter:beta
    container_name: weatherexporter
    restart: unless-stopped
    ports:
      - 9080:8080
    environment:
      - LATITUDE=42.6975
      - LONGITUDE=23.3241
      - TIMEZONE=Europe/Sofia
    networks:
       - diagnostic
```

## Configuration

| Variable    | Default        | Description                                                                 |
|-------------|----------------|-----------------------------------------------------------------------------|
| `LATITUDE`  | `42.6975`      | Latitude in decimal degrees.                                                |
| `LONGITUDE` | `23.3241`      | Longitude in decimal degrees.                                               |
| `TIMEZONE`  | `Europe/Sofia` | IANA timezone. Used so hourly/daily forecast timestamps line up with local. |

That's the entire knob surface. The set of Open-Meteo fields requested is hard-coded in the binary to a comprehensive superset; whichever fields the API actually returns for your coordinates become Prometheus metrics on the next scrape.

## Metric families at a glance

| Prefix              | Source                    | Label                    |
|---------------------|---------------------------|--------------------------|
| `t_<field>`         | Forecast API — current    | none                     |
| `t_forecast_<f>`    | Forecast API — hourly     | `hour_offset="0..47"`    |
| `t_daily_<f>`       | Forecast API — daily      | `day_offset="0..6"`      |
| `a_<field>`         | Air-Quality API — current | none                     |
| `a_forecast_<f>`    | Air-Quality API — hourly  | `hour_offset="0..47"`    |

Any ISO-time string Open-Meteo returns (e.g. `time`, `sunrise`, `sunset`) is also exposed as a `_seconds` companion gauge in unix seconds, so Grafana can plot future-time series. See [metrics.md](./metrics.md#grafana-plotting-a-real-forecast-curve) for the full Grafana recipe.

### Tiny sample of what `/metrics` looks like

```
# Current
t_temperature_2m 16.5
t_apparent_temperature 14.7
t_relative_humidity_2m 52
t_weather_code 0
a_european_aqi 26
a_us_aqi 40

# Hourly forecast (48 entries per field)
t_forecast_temperature_2m{hour_offset="0"} 17.6
t_forecast_temperature_2m{hour_offset="1"} 16.9
...
t_forecast_precipitation_probability{hour_offset="15"} 83
t_forecast_time_seconds{hour_offset="0"} 1.7785332e+09

# Daily forecast (7 entries per field)
t_daily_temperature_2m_max{day_offset="0"} 23.4
t_daily_precipitation_probability_max{day_offset="0"} 83
t_daily_sunrise_seconds{day_offset="0"} 1.77855522e+09
t_daily_sunset_seconds{day_offset="0"} 1.77860748e+09
```

Full reference is in [metrics.md](./metrics.md).

## Grafana

For a real forecast curve (because Prometheus stores every sample at "now"):

1. Query A: the metric (e.g. `t_forecast_temperature_2m`).
2. Query B (eye off): `t_forecast_time_seconds`.
3. Transformations: **Labels to fields** → **Join by field** (`hour_offset`) → **Convert field type** on the time-seconds field to **Time**.
4. Panel X-axis: that converted time field.

Same recipe for `t_daily_*` (use `day_offset` + `t_daily_time_seconds`) and `a_forecast_*` (use `hour_offset` + `a_forecast_time_seconds`).

A few quick PromQL examples:

```promql
# Chance of rain in the next hour
t_forecast_precipitation_probability{hour_offset="0"}

# Peak rain probability in the next 48 h
max(t_forecast_precipitation_probability)

# 7-day high temperatures
t_daily_temperature_2m_max

# Sunrise today
t_daily_sunrise_seconds{day_offset="0"}

# Trigger alert if European AQI > 60
a_european_aqi > 60
```

## Build it yourself

```bash
git clone https://github.com/gvltodorov/WeatherExporrter.git
cd WeatherExporrter
docker build -t weather-exporter:dev .
docker run --rm -p 9080:8080 \
  -e LATITUDE=42.6975 -e LONGITUDE=23.3241 -e TIMEZONE=Europe/Sofia \
  weather-exporter:dev
```

The binary embeds Go's `time/tzdata` so `TIMEZONE` works on any base image (Alpine, scratch, distroless) without an extra `apk add tzdata`.

## Design notes

- **Single shared `http.Client`** with a 15 s timeout — no goroutine / socket pile-up if Open-Meteo is slow.
- **Response body capped at 1 MiB** via `io.LimitReader`, plus streaming `json.Decoder` to avoid an intermediate byte buffer.
- **Labeled `GaugeVec`s are `Reset()`-cleared every scrape** so a value Open-Meteo stops returning (per-hour `null`, shorter array, dropped field) disappears from `/metrics` instead of going stale.
- **`hour_offset` / `day_offset` labels only** — never the timestamp itself, so cardinality is fixed (`forecastHours=48`, `forecastDays=7`).
- Timezone is parsed **once** at startup using the embedded IANA database; not re-loaded per scrape.

## License

See repository.
