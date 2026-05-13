# Metrics reference

This document lists every metric the exporter can expose. The exporter requests the full superset of fields from Open-Meteo on every scrape; if Open-Meteo doesn't have data for a particular field at your coordinates, that metric is simply absent.

> **Scrape**: `GET http://<host>:8080/metrics`
> **Inputs**: only `LATITUDE`, `LONGITUDE`, `TIMEZONE` env vars.
> **Forecast horizon**: 48 hourly entries, 7 daily entries (hard-coded; bounded label cardinality).

---

## Naming convention

| Prefix              | Source                    | Type            | Label             |
|---------------------|---------------------------|-----------------|-------------------|
| `t_<field>`         | Forecast API – current    | scalar gauge    | –                 |
| `t_forecast_<f>`    | Forecast API – hourly     | labeled gauge   | `hour_offset="0..47"` |
| `t_daily_<f>`       | Forecast API – daily      | labeled gauge   | `day_offset="0..6"`   |
| `a_<field>`         | Air-Quality API – current | scalar gauge    | –                 |
| `a_forecast_<f>`    | Air-Quality API – hourly  | labeled gauge   | `hour_offset="0..47"` |

Any string field returned by Open-Meteo that parses as an ISO timestamp (date or date+hour) is exposed with a parallel `_seconds` companion metric whose value is unix-seconds (e.g. `t_daily_sunrise_seconds`). These are essential for plotting future timestamps in Grafana.

---

## Units cheat sheet

| Family           | Unit                      |
|------------------|---------------------------|
| Temperatures     | °C                        |
| Apparent / dew   | °C                        |
| Humidity         | % (0–100)                 |
| Pressure         | hPa                       |
| Wind speed       | km/h                      |
| Wind direction   | degrees (0=N, 90=E, …)    |
| Precipitation    | mm                        |
| Snowfall         | cm                        |
| Snow depth       | m                         |
| Cloud cover      | % (0–100)                 |
| Visibility       | m                         |
| Solar radiation  | W/m²                      |
| Daily radiation  | MJ/m²                     |
| UV index         | 0–11+ (unitless)          |
| AQI              | 0–500 (US) / 0–100+ (EU)  |
| Pollutants       | µg/m³                     |
| Pollen           | grains/m³                 |
| Soil moisture    | m³/m³ (0.0–~0.5)          |
| Sunshine / daylight | seconds                |
| Times            | unix seconds              |
| `is_day`         | 0 = night, 1 = day        |

---

## Current weather (`t_*`)

| Metric                              | Unit  | Notes                                                  |
|-------------------------------------|-------|--------------------------------------------------------|
| `t_temperature_2m`                  | °C    | Air temperature at 2 m above ground                    |
| `t_apparent_temperature`            | °C    | Feels-like temperature (combines wind + humidity)      |
| `t_dew_point_2m`                    | °C    |                                                        |
| `t_relative_humidity_2m`            | %     | 0–100                                                  |
| `t_pressure_msl`                    | hPa   | Mean sea level pressure                                |
| `t_surface_pressure`                | hPa   | Pressure at station altitude                           |
| `t_precipitation`                   | mm    | Total water equivalent in the last interval            |
| `t_rain`                            | mm    | Rain only                                              |
| `t_showers`                         | mm    | Convective showers                                     |
| `t_snowfall`                        | cm    | Snow in the last interval                              |
| `t_cloud_cover`                     | %     | Total cloud cover                                      |
| `t_cloud_cover_low`                 | %     | Cloud cover < ~2 km                                    |
| `t_cloud_cover_mid`                 | %     | Cloud cover ~2–6 km                                    |
| `t_cloud_cover_high`                | %     | Cloud cover > ~6 km                                    |
| `t_wind_speed_10m`                  | km/h  |                                                        |
| `t_wind_direction_10m`              | deg   |                                                        |
| `t_wind_gusts_10m`                  | km/h  |                                                        |
| `t_weather_code`                    | int   | WMO weather code – see table below                     |
| `t_is_day`                          | 0/1   | 1 during local daylight                                |
| `t_interval`                        | s     | Sampling interval of the upstream "current" block      |
| `t_time_seconds`                    | unix  | Timestamp of the upstream "current" sample             |

---

## Hourly forecast (`t_forecast_*`, label `hour_offset="0..47"`)

All current-weather metrics above also exist as forecast series. Additional hourly-only fields:

| Metric                                      | Unit    | Notes                                                                |
|---------------------------------------------|---------|----------------------------------------------------------------------|
| `t_forecast_precipitation_probability`      | %       | Likelihood of any precipitation (rain, showers, snow combined)       |
| `t_forecast_snow_depth`                     | m       | Accumulated snow on the ground                                       |
| `t_forecast_visibility`                     | m       | Horizontal visibility                                                |
| `t_forecast_evapotranspiration`             | mm      | Hourly ET                                                            |
| `t_forecast_et0_fao_evapotranspiration`     | mm      | Reference evapotranspiration (FAO-56)                                |
| `t_forecast_vapour_pressure_deficit`        | kPa     | Useful for plant transpiration / mold risk                           |
| `t_forecast_wind_speed_80m`                 | km/h    |                                                                      |
| `t_forecast_wind_speed_120m`                | km/h    |                                                                      |
| `t_forecast_wind_speed_180m`                | km/h    |                                                                      |
| `t_forecast_wind_direction_80m`             | deg     |                                                                      |
| `t_forecast_wind_direction_120m`            | deg     |                                                                      |
| `t_forecast_wind_direction_180m`            | deg     |                                                                      |
| `t_forecast_uv_index`                       | 0–11+   |                                                                      |
| `t_forecast_uv_index_clear_sky`             | 0–11+   | UV assuming a clear sky                                              |
| `t_forecast_sunshine_duration`              | s       | Sunshine seconds within that hour (0–3600)                           |
| `t_forecast_freezing_level_height`          | m       | Altitude of 0 °C isotherm                                            |
| `t_forecast_cape`                           | J/kg    | Convective Available Potential Energy (storm fuel)                   |
| `t_forecast_lifted_index`                   | –       | Stability index; ≤ 0 indicates thunderstorm potential                |
| `t_forecast_convective_inhibition`          | J/kg    | Energy that suppresses convection                                    |
| `t_forecast_shortwave_radiation`            | W/m²    | Total shortwave incoming solar radiation                             |
| `t_forecast_direct_radiation`               | W/m²    | Direct beam on a horizontal surface                                  |
| `t_forecast_diffuse_radiation`              | W/m²    |                                                                      |
| `t_forecast_direct_normal_irradiance`       | W/m²    | DNI – energy on a surface normal to the sun                          |
| `t_forecast_terrestrial_radiation`          | W/m²    | Top-of-atmosphere reference                                          |
| `t_forecast_soil_temperature_0cm`           | °C      | Surface                                                              |
| `t_forecast_soil_temperature_6cm`           | °C      |                                                                      |
| `t_forecast_soil_temperature_18cm`          | °C      |                                                                      |
| `t_forecast_soil_temperature_54cm`          | °C      |                                                                      |
| `t_forecast_soil_moisture_0_to_1cm`         | m³/m³   | 0.0–~0.5                                                             |
| `t_forecast_soil_moisture_1_to_3cm`         | m³/m³   |                                                                      |
| `t_forecast_soil_moisture_3_to_9cm`         | m³/m³   |                                                                      |
| `t_forecast_soil_moisture_9_to_27cm`        | m³/m³   |                                                                      |
| `t_forecast_soil_moisture_27_to_81cm`       | m³/m³   |                                                                      |
| `t_forecast_time_seconds`                   | unix    | Target timestamp of each `hour_offset` (use as X-axis in Grafana)    |

---

## Daily forecast (`t_daily_*`, label `day_offset="0..6"`)

| Metric                                          | Unit    | Notes                                       |
|-------------------------------------------------|---------|---------------------------------------------|
| `t_daily_temperature_2m_max`                    | °C      | Daily high                                  |
| `t_daily_temperature_2m_min`                    | °C      | Daily low                                   |
| `t_daily_apparent_temperature_max`              | °C      | Feels-like high                             |
| `t_daily_apparent_temperature_min`              | °C      | Feels-like low                              |
| `t_daily_precipitation_sum`                     | mm      |                                             |
| `t_daily_rain_sum`                              | mm      |                                             |
| `t_daily_showers_sum`                           | mm      |                                             |
| `t_daily_snowfall_sum`                          | cm      |                                             |
| `t_daily_precipitation_hours`                   | h       | Number of hours with precipitation          |
| `t_daily_precipitation_probability_max`         | %       | Highest hourly probability that day         |
| `t_daily_uv_index_max`                          | 0–11+   | Daily peak                                  |
| `t_daily_uv_index_clear_sky_max`                | 0–11+   |                                             |
| `t_daily_wind_speed_10m_max`                    | km/h    |                                             |
| `t_daily_wind_gusts_10m_max`                    | km/h    |                                             |
| `t_daily_wind_direction_10m_dominant`           | deg     | Dominant wind direction                     |
| `t_daily_shortwave_radiation_sum`               | MJ/m²   | Energy sum                                  |
| `t_daily_et0_fao_evapotranspiration`            | mm      | FAO reference ET                            |
| `t_daily_sunshine_duration`                     | s       | Total sunshine duration that day            |
| `t_daily_daylight_duration`                     | s       | Sunrise → sunset interval                   |
| `t_daily_weather_code`                          | int     | WMO daily summary code                      |
| `t_daily_time_seconds`                          | unix    | Start of the day (00:00 local), per `day_offset` |
| `t_daily_sunrise_seconds`                       | unix    | Sunrise for that day                        |
| `t_daily_sunset_seconds`                        | unix    | Sunset for that day                         |

---

## Current air quality (`a_*`)

| Metric                                          | Unit       | Notes                                        |
|-------------------------------------------------|------------|----------------------------------------------|
| `a_pm2_5`                                       | µg/m³      | Fine particulate matter                      |
| `a_pm10`                                        | µg/m³      | Coarse particulate matter                    |
| `a_carbon_monoxide`                             | µg/m³      |                                              |
| `a_nitrogen_dioxide`                            | µg/m³      |                                              |
| `a_sulphur_dioxide`                             | µg/m³      |                                              |
| `a_ozone`                                       | µg/m³      |                                              |
| `a_ammonia`                                     | µg/m³      |                                              |
| `a_dust`                                        | µg/m³      | Mineral dust (e.g. Saharan)                  |
| `a_aerosol_optical_depth`                       | –          | 0.0–~1.0; haze indicator                     |
| `a_european_aqi`                                | 0–100+     | EU AQI overall – see scale below             |
| `a_european_aqi_pm2_5`                          | 0–100+     | Per-pollutant breakdowns                     |
| `a_european_aqi_pm10`                           | 0–100+     |                                              |
| `a_european_aqi_nitrogen_dioxide`               | 0–100+     |                                              |
| `a_european_aqi_ozone`                          | 0–100+     |                                              |
| `a_european_aqi_sulphur_dioxide`                | 0–100+     |                                              |
| `a_us_aqi`                                      | 0–500      | US AQI overall – see scale below             |
| `a_us_aqi_pm2_5`                                | 0–500      |                                              |
| `a_us_aqi_pm10`                                 | 0–500      |                                              |
| `a_us_aqi_nitrogen_dioxide`                     | 0–500      |                                              |
| `a_us_aqi_carbon_monoxide`                      | 0–500      |                                              |
| `a_us_aqi_ozone`                                | 0–500      |                                              |
| `a_us_aqi_sulphur_dioxide`                      | 0–500      |                                              |
| `a_uv_index`                                    | 0–11+      |                                              |
| `a_uv_index_clear_sky`                          | 0–11+      |                                              |
| `a_alder_pollen`                                | grains/m³  | Europe only                                  |
| `a_birch_pollen`                                | grains/m³  | Europe only                                  |
| `a_grass_pollen`                                | grains/m³  | Europe only                                  |
| `a_mugwort_pollen`                              | grains/m³  | Europe only                                  |
| `a_olive_pollen`                                | grains/m³  | Europe / Mediterranean                       |
| `a_ragweed_pollen`                              | grains/m³  | Europe only                                  |
| `a_interval`                                    | s          | Upstream sampling interval                   |
| `a_time_seconds`                                | unix       | Timestamp of the "current" AQI sample        |

## Hourly air-quality forecast (`a_forecast_*`, label `hour_offset="0..47"`)

Every `a_*` metric above (except `a_interval`) has a corresponding `a_forecast_*` series with an `hour_offset` label. Plus:

| Metric                              | Unit   | Notes                                                   |
|-------------------------------------|--------|---------------------------------------------------------|
| `a_forecast_time_seconds`           | unix   | Target timestamp per `hour_offset` – Grafana X-axis     |

---

## WMO weather codes (`*_weather_code`)

| Code   | Meaning                            |
|--------|------------------------------------|
| 0      | Clear sky                          |
| 1      | Mainly clear                       |
| 2      | Partly cloudy                      |
| 3      | Overcast                           |
| 45, 48 | Fog / depositing rime fog          |
| 51, 53, 55 | Drizzle (light / moderate / dense) |
| 56, 57 | Freezing drizzle                   |
| 61, 63, 65 | Rain (slight / moderate / heavy)  |
| 66, 67 | Freezing rain                      |
| 71, 73, 75 | Snowfall                          |
| 77     | Snow grains                        |
| 80, 81, 82 | Rain showers                      |
| 85, 86 | Snow showers                       |
| 95     | Thunderstorm                       |
| 96, 99 | Thunderstorm with hail             |

Use Grafana **Value mappings** to render an icon or label per code.

---

## AQI scales

**European AQI** (`a_european_aqi*`):

| Range  | Quality     |
|--------|-------------|
| 0–20   | Good        |
| 20–40  | Fair        |
| 40–60  | Moderate    |
| 60–80  | Poor        |
| 80–100 | Very poor   |
| 100+   | Extremely poor |

**US AQI** (`a_us_aqi*`):

| Range   | Quality                          |
|---------|----------------------------------|
| 0–50    | Good                             |
| 51–100  | Moderate                         |
| 101–150 | Unhealthy for sensitive groups   |
| 151–200 | Unhealthy                        |
| 201–300 | Very unhealthy                   |
| 301–500 | Hazardous                        |

These ranges work as Grafana thresholds out of the box.

---

## Useful PromQL snippets

```promql
# Right-now temperature
t_temperature_2m

# Chance of rain in the next hour
t_forecast_precipitation_probability{hour_offset="0"}

# Peak rain probability in the next 48 h
max(t_forecast_precipitation_probability)

# Hour with the highest precipitation probability
topk(1, t_forecast_precipitation_probability)

# 7-day high temperatures
t_daily_temperature_2m_max

# Is it likely to thunder in the next 24 h? (CAPE > 1000, LI < 0)
max(t_forecast_cape and on(hour_offset) t_forecast_lifted_index < 0)

# Total precipitation forecast in next 48 h
sum(t_forecast_precipitation)

# Sunrise time today
t_daily_sunrise_seconds{day_offset="0"}

# Current AQI category trigger (alerting): EU AQI > 60
a_european_aqi > 60
```

---

## Grafana: plotting a real forecast curve

Prometheus stores all forecast samples at "now", so a naive `t_forecast_*` query plotted on a time-series panel will collapse all hours onto the current time. To get a real future-time curve:

1. **Query A** (visible): the metric you want, e.g. `t_forecast_temperature_2m`.
2. **Query B** (eye off): `t_forecast_time_seconds` (or `t_daily_time_seconds`, or `a_forecast_time_seconds`, matching the family).
3. **Transformations**:
   - **Labels to fields** on both queries (key: `hour_offset` / `day_offset`).
   - **Join by field** on that label.
   - **Convert field type** → set the time-seconds field to **Time**.
4. **Panel options**: X-axis = the converted time field.

Pair with sunrise/sunset markers using `t_daily_sunrise_seconds` and `t_daily_sunset_seconds` for a polished weather dashboard.

---

## Cardinality footprint

For a location where Open-Meteo returns the full set:

| Block                | Approx series |
|----------------------|---------------|
| `t_*` current        | ~19           |
| `t_forecast_*`       | ~48 fields × 48 hours ≈ 2 304 |
| `t_daily_*`          | ~25 fields × 7 days ≈ 175     |
| `a_*` current        | ~30           |
| `a_forecast_*`       | ~30 fields × 48 hours ≈ 1 440 |
| **Total**            | **~4 000 series** |

The total is fixed at startup (no growth across scrapes) and tightly bounded — every labeled gauge is `Reset()`-cleared at the start of each scrape.

---

## Absent or null metrics

- Pollen series (`*_pollen`) are only emitted for European coordinates.
- `t_interval` / `a_interval` only appear when Open-Meteo includes that field in the "current" block.
- For each hour where Open-Meteo returns `null`, that single `hour_offset` is absent for that scrape — Grafana shows a gap, which is the correct signal.
- If Open-Meteo drops a field entirely, the metric name disappears from `/metrics`. This is intentional ("if it's available" semantics).
