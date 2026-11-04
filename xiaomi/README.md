# xiaomi exporter

Prometheus exporter for a Xiaomi air purifier or humidifier on the local network, read over the miio protocol.

**Image** `ghcr.io/gvltodorov/exporrters/xiaomi:latest` · **Port** `8000` · [All exporters](../README.md)

## Run

```yaml
services:
  xiaomi-exporter:
    image: ghcr.io/gvltodorov/exporrters/xiaomi:latest
    restart: unless-stopped
    ports:
      - 8086:8000
    environment:
      - IP=192.168.1.30
      - TOKEN=change-me
```

Get the device token with
[Xiaomi Cloud Tokens Extractor](https://github.com/PiotrMachowski/Xiaomi-cloud-tokens-extractor).

## Configuration

| Variable | Default | Description |
|---|---|---|
| `IP` | *(required)* | Device IP address |
| `TOKEN` | *(required)* | 32-character miio device token |
| `PORT` | *(required outside Docker)* | Port for `/metrics`. The image always listens on `8000` |

## Metrics

| Metric | Description |
|---|---|
| `temperature_celsius` | Temperature |
| `humidity_percent` | Relative humidity |
| `aqi_index` | Current air quality index |
| `avarage_aqi_index` | Average air quality index |
| `volume` | Buzzer volume |
| `filter_hours_used` | Filter usage in hours |

## Notes

- The device is read every 15 seconds, or 5 seconds after a failed read.
  When a read fails, the last values stay in place.
- Uses python-miio 0.4.7 (`AirPurifier`) on Python 3.11.

## Development

```bash
pip install '.[test]'
python -m pytest -m "not leak"  # unit tests
python -m pytest -m leak        # leak tests
```
