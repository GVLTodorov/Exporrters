package main

import (
	"fmt"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// Metric descriptors. Metrics shared by several applications start with
// arr_ and carry an "app" label (always the first label); metrics specific
// to one application start with its name and have no app label.

var allDescs []*prometheus.Desc

func newDesc(name, help string, labels ...string) *prometheus.Desc {
	d := prometheus.NewDesc(name, help, labels, nil)
	allDescs = append(allDescs, d)
	return d
}

// Shared (app label first).
var (
	descUp              = newDesc("arr_up", "1 if the application's API answered the last status request, 0 if not.", "app")
	descInfo            = newDesc("arr_info", "Application version; always 1.", "app", "version")
	descRefreshDuration = newDesc("arr_refresh_duration_seconds", "How long the last refresh of this application took.", "app")
	descRefreshErrors   = newDesc("arr_refresh_errors", "Number of API requests that failed during the last refresh.", "app")
	descLastRefresh     = newDesc("arr_last_refresh_timestamp_seconds", "Unix time the last refresh finished.", "app")

	descHealthIssues = newDesc("arr_health_issues", "Number of health issues the application reports (System > Status).", "app")
	descHealthIssue  = newDesc("arr_health_issue", "One series per distinct health issue; the value is how many times it is reported.", "app", "source", "type", "message")

	descQueueTotal     = newDesc("arr_queue_total", "Items in the download queue.", "app")
	descQueueItems     = newDesc("arr_queue_items", "Queue items by queue status, tracked download status and tracked download state.", "app", "status", "download_status", "download_state")
	descQueueSize      = newDesc("arr_queue_size_bytes", "Total size of all items in the queue.", "app")
	descQueueRemaining = newDesc("arr_queue_remaining_bytes", "Bytes still to download for all items in the queue.", "app")

	descRootFree       = newDesc("arr_rootfolder_free_bytes", "Free space of a root folder.", "app", "path")
	descRootAccessible = newDesc("arr_rootfolder_accessible", "1 if the application can access the root folder, 0 if not (for example a dropped NFS mount).", "app", "path")

	descWantedMissing = newDesc("arr_wanted_missing", "Monitored items that are missing (Wanted > Missing): episodes for Sonarr, movies for Radarr.", "app")
	descWantedCutoff  = newDesc("arr_wanted_cutoff_unmet", "Items that have a file below the quality cutoff (Wanted > Cutoff Unmet).", "app")
)

// Sonarr.
var (
	descSonarrSeries          = newDesc("sonarr_series", "Series in the library.")
	descSonarrSeriesMonitored = newDesc("sonarr_series_monitored", "Monitored series.")
	descSonarrSeriesComplete  = newDesc("sonarr_series_complete", "Series with every counted episode downloaded.")
	descSonarrSeriesStatus    = newDesc("sonarr_series_by_status", "Series by airing status (continuing, ended, upcoming, deleted).", "status")
	descSonarrSeasons         = newDesc("sonarr_seasons", "Seasons across all series.")
	descSonarrEpisodes        = newDesc("sonarr_episodes", "Episodes Sonarr counts as wanted or downloaded (aired and monitored, or with a file).")
	descSonarrEpisodesAll     = newDesc("sonarr_episodes_all", "All known episodes, including unaired and unmonitored ones.")
	descSonarrEpisodeFiles    = newDesc("sonarr_episode_files", "Downloaded episode files.")
	descSonarrSize            = newDesc("sonarr_size_on_disk_bytes", "Size of all series on disk.")
)

// Radarr.
var (
	descRadarrMovies     = newDesc("radarr_movies", "Movies in the library.")
	descRadarrMonitored  = newDesc("radarr_movies_monitored", "Monitored movies.")
	descRadarrDownloaded = newDesc("radarr_movies_downloaded", "Movies with a file.")
	descRadarrStatus     = newDesc("radarr_movies_by_status", "Movies by release status (tba, announced, inCinemas, released, deleted).", "status")
	descRadarrQuality    = newDesc("radarr_movies_by_quality", "Downloaded movies by file quality.", "quality")
	descRadarrSize       = newDesc("radarr_size_on_disk_bytes", "Size of all movies on disk.")
)

// Prowlarr.
var (
	descProwlarrIndexers        = newDesc("prowlarr_indexers", "Configured indexers.")
	descProwlarrIndexersEnabled = newDesc("prowlarr_indexers_enabled", "Enabled indexers.")
	descProwlarrIndexerEnabled  = newDesc("prowlarr_indexer_enabled", "1 if the indexer is enabled, 0 if not.", "indexer", "protocol", "privacy")
	descProwlarrIndexerDisabled = newDesc("prowlarr_indexer_disabled", "1 if Prowlarr has temporarily disabled the indexer after repeated failures.", "indexer")
	descProwlarrResponseTime    = newDesc("prowlarr_indexer_response_time_ms", "Average response time of the indexer.", "indexer")
	descProwlarrQueries         = newDesc("prowlarr_indexer_queries_total", "Search queries sent to the indexer (all time, from Prowlarr's history).", "indexer")
	descProwlarrGrabs           = newDesc("prowlarr_indexer_grabs_total", "Releases grabbed from the indexer.", "indexer")
	descProwlarrRSS             = newDesc("prowlarr_indexer_rss_queries_total", "RSS queries sent to the indexer.", "indexer")
	descProwlarrAuth            = newDesc("prowlarr_indexer_auth_queries_total", "Authentication queries sent to the indexer.", "indexer")
	descProwlarrFailedQueries   = newDesc("prowlarr_indexer_failed_queries_total", "Failed search queries.", "indexer")
	descProwlarrFailedGrabs     = newDesc("prowlarr_indexer_failed_grabs_total", "Failed grabs.", "indexer")
	descProwlarrFailedRSS       = newDesc("prowlarr_indexer_failed_rss_queries_total", "Failed RSS queries.", "indexer")
	descProwlarrFailedAuth      = newDesc("prowlarr_indexer_failed_auth_queries_total", "Failed authentication queries.", "indexer")
	descProwlarrUAQueries       = newDesc("prowlarr_user_agent_queries_total", "Queries made through Prowlarr, by client (Sonarr, Radarr, ...).", "user_agent")
	descProwlarrUAGrabs         = newDesc("prowlarr_user_agent_grabs_total", "Grabs made through Prowlarr, by client.", "user_agent")
)

// Bazarr.
var (
	descBazarrMissing   = newDesc("bazarr_missing_subtitles", "Episodes or movies with missing subtitles (Wanted).", "type")
	descBazarrThrottled = newDesc("bazarr_throttled_providers", "Subtitle providers currently throttled.")
	descBazarrSignalr   = newDesc("bazarr_signalr_connected", "1 if Bazarr's live connection to Sonarr or Radarr is up.", "target")
	descBazarrHistory   = newDesc("bazarr_history_records", "Subtitle history records (downloads, upgrades, manual actions).", "type")
)

// sink collects the metrics of one refresh of one application.
type sink struct {
	app     string
	up      bool
	metrics []prometheus.Metric
	errs    []string
}

func (s *sink) add(d *prometheus.Desc, t prometheus.ValueType, v float64, labels ...string) {
	s.metrics = append(s.metrics, prometheus.MustNewConstMetric(d, t, v, labels...))
}

// gauge adds an application-specific gauge (no app label).
func (s *sink) gauge(d *prometheus.Desc, v float64, labels ...string) {
	s.add(d, prometheus.GaugeValue, v, labels...)
}

// counter adds an application-specific counter (no app label).
func (s *sink) counter(d *prometheus.Desc, v float64, labels ...string) {
	s.add(d, prometheus.CounterValue, v, labels...)
}

// shared adds an arr_* gauge, prepending the app label.
func (s *sink) shared(d *prometheus.Desc, v float64, labels ...string) {
	s.add(d, prometheus.GaugeValue, v, append([]string{s.app}, labels...)...)
}

// fail records a failed request; the refresh carries on with the next one.
func (s *sink) fail(err error) {
	s.errs = append(s.errs, err.Error())
}

func (s *sink) errorSummary() string {
	return fmt.Sprintf("%s: %d request(s) failed: %s", s.app, len(s.errs), strings.Join(s.errs, "; "))
}

// emitCounts emits one series per key with the number of occurrences, which
// both aggregates and de-duplicates: two identical label sets in one
// scrape would make the whole /metrics request fail.
func emitCounts[K comparable](m map[K]int, emit func(key K, n float64)) {
	for k, n := range m {
		emit(k, float64(n))
	}
}

func boolFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
