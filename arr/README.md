# arr exporter

Prometheus exporter for Sonarr, Radarr, Prowlarr and Bazarr: library, queue, health, indexer and subtitle stats from one container.

**Image** `ghcr.io/gvltodorov/arrexporrter:latest` · **Port** `8080` · [All exporters](../README.md)

## Run

```yaml
services:
  arr-exporter:
    image: ghcr.io/gvltodorov/arrexporrter:latest
    restart: unless-stopped
    ports:
      - 9707:8080
    environment:
      - SONARR_URL=192.168.1.10:8989
      - SONARR_API_KEY=change-me
      - RADARR_URL=192.168.1.10:7878
      - RADARR_API_KEY=change-me
      - PROWLARR_URL=192.168.1.10:9696
      - PROWLARR_API_KEY=change-me
      - BAZARR_URL=192.168.1.10:6767
      - BAZARR_API_KEY=change-me
```

Each app's API key is under **Settings > General > Security** (Sonarr,
Radarr, Prowlarr) or **Settings > General > Security > API Key** (Bazarr).

## Configuration

An app is exported when both its `_URL` and `_API_KEY` are set, and skipped
when neither is. Setting only one of the two is a startup error. At least
one app is required.

| Variable | Default | Description |
|---|---|---|
| `SONARR_URL`, `SONARR_API_KEY` | *(unset)* | Sonarr address and API key |
| `RADARR_URL`, `RADARR_API_KEY` | *(unset)* | Radarr address and API key |
| `PROWLARR_URL`, `PROWLARR_API_KEY` | *(unset)* | Prowlarr address and API key |
| `BAZARR_URL`, `BAZARR_API_KEY` | *(unset)* | Bazarr address and API key |
| `REFRESH_INTERVAL` | `30s` | How often each app is polled |
| `REQUEST_TIMEOUT` | `10s` | Timeout for one API request |
| `LISTEN_ADDR` | `:8080` | Address the exporter listens on |

A URL can be a bare `host:port` (`http://` is assumed) or a full URL with
a base path, such as `https://example.lan/sonarr`.

## Metrics

Metrics shared by several apps start with `arr_` and have an `app` label
(`sonarr`, `radarr`, `prowlarr` or `bazarr`).

| Metric | Labels | Apps | Description |
|---|---|---|---|
| `arr_up` | `app` | all | 1 if the app's status endpoint answered on the last refresh |
| `arr_info` | `app`, `version` | all | App version (always 1) |
| `arr_refresh_errors` | `app` | all | API requests that failed in the last refresh |
| `arr_refresh_duration_seconds` | `app` | all | Duration of the last refresh |
| `arr_last_refresh_timestamp_seconds` | `app` | all | When the last refresh finished |
| `arr_health_issues` | `app` | all | Number of health issues (System > Status) |
| `arr_health_issue` | `app`, `source`, `type`, `message` | all | One series per health issue |
| `arr_queue_total` | `app` | sonarr, radarr | Items in the download queue |
| `arr_queue_items` | `app`, `status`, `download_status`, `download_state` | sonarr, radarr | Queue items by state (for example `importBlocked`) |
| `arr_queue_size_bytes` | `app` | sonarr, radarr | Total size of queued downloads |
| `arr_queue_remaining_bytes` | `app` | sonarr, radarr | Bytes left to download |
| `arr_rootfolder_free_bytes` | `app`, `path` | sonarr, radarr | Free space of each root folder |
| `arr_rootfolder_accessible` | `app`, `path` | sonarr, radarr | 0 when the app can't reach a root folder (such as a dropped NFS mount) |
| `arr_wanted_missing` | `app` | sonarr, radarr | Wanted > Missing: episodes for Sonarr, movies for Radarr |
| `arr_wanted_cutoff_unmet` | `app` | sonarr, radarr | Wanted > Cutoff Unmet |
| `sonarr_series`, `sonarr_series_monitored`, `sonarr_series_complete` | | sonarr | Series in the library, monitored, and fully downloaded |
| `sonarr_series_by_status` | `status` | sonarr | Series by airing status |
| `sonarr_seasons` | | sonarr | Seasons across all series |
| `sonarr_episodes` | | sonarr | Episodes Sonarr counts (aired and monitored, or with a file) |
| `sonarr_episodes_all` | | sonarr | All known episodes, including unaired |
| `sonarr_episode_files` | | sonarr | Downloaded episode files |
| `sonarr_size_on_disk_bytes` | | sonarr | Size of all series |
| `radarr_movies`, `radarr_movies_monitored`, `radarr_movies_downloaded` | | radarr | Movies in the library, monitored, and with a file |
| `radarr_movies_by_status` | `status` | radarr | Movies by release status |
| `radarr_movies_by_quality` | `quality` | radarr | Downloaded movies by file quality |
| `radarr_size_on_disk_bytes` | | radarr | Size of all movies |
| `prowlarr_indexers`, `prowlarr_indexers_enabled` | | prowlarr | Configured and enabled indexers |
| `prowlarr_indexer_enabled` | `indexer`, `protocol`, `privacy` | prowlarr | 1 if the indexer is enabled |
| `prowlarr_indexer_disabled` | `indexer` | prowlarr | 1 while Prowlarr has backed off the indexer after failures |
| `prowlarr_indexer_response_time_ms` | `indexer` | prowlarr | Average response time |
| `prowlarr_indexer_{queries,grabs,rss_queries,auth_queries}_total` | `indexer` | prowlarr | Requests and grabs per indexer |
| `prowlarr_indexer_failed_{queries,grabs,rss_queries,auth_queries}_total` | `indexer` | prowlarr | Failures per indexer |
| `prowlarr_user_agent_{queries,grabs}_total` | `user_agent` | prowlarr | Requests and grabs per client (Sonarr, Radarr, ...) |
| `bazarr_missing_subtitles` | `type` | bazarr | Episodes and movies with missing subtitles |
| `bazarr_throttled_providers` | | bazarr | Subtitle providers currently throttled |
| `bazarr_signalr_connected` | `target` | bazarr | 1 if Bazarr's live link to Sonarr or Radarr is up |
| `bazarr_history_records` | `type` | bazarr | Subtitle history records |

## Notes

- Each app is polled on its own schedule, and `/metrics` serves the latest
  result. A slow or unreachable app never delays the scrape or the other
  apps; it shows `arr_up 0` instead.
- A failed endpoint costs only its own metrics; `arr_refresh_errors`
  counts them and the exporter logs the reason.
- The API key is sent in the `X-Api-Key` header, never in a URL or log line.
- Prowlarr's `_total` counters are all-time totals from its history, so
  they drop back if that history is cleared. Use `increase()` or `rate()`.
- Sonarr's per-episode quality breakdown is not exported. It would need
  one request per series on every refresh.
- The choice of endpoints and metrics follows
  [exportarr](https://github.com/onedr0p/exportarr) (MIT); see
  [NOTICE](NOTICE).

## Development

```bash
go test ./... -race -skip Leak    # unit tests (100% statement coverage)
go test ./... -run Leak -count=3  # leak tests
```
