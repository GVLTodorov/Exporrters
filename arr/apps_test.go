package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestCollectSonarr(t *testing.T) {
	s, got := collectWith(t, collectSonarr, newFakeAPI(sonarrFixture), "")
	if !s.up || len(s.errs) != 0 {
		t.Fatalf("up=%v errs=%v, want up and no errors", s.up, s.errs)
	}
	expect(t, got, map[string]float64{
		`arr_info{app="test",version="4.0.20.3014"}`: 1,
		`arr_health_issues{app="test"}`:              3,
		`arr_health_issue{app="test",message="Indexers unavailable: A",source="IndexerStatusCheck",type="warning"}`: 2,
		`arr_health_issue{app="test",message="Missing root folder: /tv",source="RootFolderCheck",type="error"}`:     1,
		`arr_queue_total{app="test"}`:           3,
		`arr_queue_size_bytes{app="test"}`:      6000,
		`arr_queue_remaining_bytes{app="test"}`: 1400,
		`arr_queue_items{app="test",download_state="downloading",download_status="ok",status="downloading"}`:       2,
		`arr_queue_items{app="test",download_state="importBlocked",download_status="warning",status="completed"}`: 1,
		`arr_rootfolder_free_bytes{app="test",path="/series"}`:  2059317805056,
		`arr_rootfolder_accessible{app="test",path="/series"}`:  1,
		`arr_rootfolder_free_bytes{app="test",path="/nfs"}`:     0,
		`arr_rootfolder_accessible{app="test",path="/nfs"}`:     0,
		`arr_wanted_missing{app="test"}`:                        5,
		`arr_wanted_cutoff_unmet{app="test"}`:                   2,
		`sonarr_series`:                                         3,
		`sonarr_series_monitored`:                               2,
		`sonarr_series_complete`:                                1,
		`sonarr_series_by_status{status="continuing"}`:          2,
		`sonarr_series_by_status{status="ended"}`:               1,
		`sonarr_seasons`:                                        6,
		`sonarr_episodes`:                                       32,
		`sonarr_episodes_all`:                                   42,
		`sonarr_episode_files`:                                  26,
		`sonarr_size_on_disk_bytes`:                             57000,
	})
}

func TestCollectRadarr(t *testing.T) {
	f := newFakeAPI(radarrFixture)
	var query string
	f.handle("/movie", func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = w.Write([]byte(radarrFixture["/movie"]))
	})
	s, got := collectWith(t, collectRadarr, f, "")
	if !s.up || len(s.errs) != 0 {
		t.Fatalf("up=%v errs=%v, want up and no errors", s.up, s.errs)
	}
	if query != "excludeLocalCovers=true" {
		t.Errorf("movie query = %q, want excludeLocalCovers=true", query)
	}
	expect(t, got, map[string]float64{
		`arr_info{app="test",version="7.0.3.10918"}`:          1,
		`arr_health_issues{app="test"}`:                       0,
		`arr_queue_total{app="test"}`:                         0,
		`arr_queue_size_bytes{app="test"}`:                    0,
		`arr_rootfolder_free_bytes{app="test",path="/movies"}`: 123,
		`arr_wanted_missing{app="test"}`:                      1,
		`arr_wanted_cutoff_unmet{app="test"}`:                 0,
		`radarr_movies`:                                       5,
		`radarr_movies_monitored`:                             4,
		`radarr_movies_downloaded`:                            4,
		`radarr_size_on_disk_bytes`:                           14000,
		`radarr_movies_by_status{status="released"}`:          3,
		`radarr_movies_by_status{status="inCinemas"}`:         1,
		`radarr_movies_by_status{status="announced"}`:         1,
		`radarr_movies_by_quality{quality="Bluray-1080p"}`:    2,
		`radarr_movies_by_quality{quality="WEBDL-2160p"}`:     1,
	})
	// An empty queue and no health issues leave no per-item series behind.
	expectAbsent(t, got, "arr_queue_items", "arr_health_issue")
	if n := countPrefix(got, "radarr_movies_by_quality"); n != 2 {
		t.Errorf("%d quality series, want 2 (a movie with hasFile but no movieFile is skipped)", n)
	}
}

func TestCollectProwlarr(t *testing.T) {
	s, got := collectWith(t, collectProwlarr, newFakeAPI(prowlarrFixture), "")
	if !s.up || len(s.errs) != 0 {
		t.Fatalf("up=%v errs=%v, want up and no errors", s.up, s.errs)
	}
	expect(t, got, map[string]float64{
		`arr_info{app="test",version="3.1.0.5799"}`: 1,
		`arr_health_issues{app="test"}`:             1,
		`prowlarr_indexers`:                         4,
		`prowlarr_indexers_enabled`:                 3,
		`prowlarr_indexer_enabled{indexer="X",privacy="public",protocol="torrent"}`:  1,
		`prowlarr_indexer_enabled{indexer="Y",privacy="private",protocol="torrent"}`: 0,
		`prowlarr_indexer_enabled{indexer="Z",privacy="private",protocol="usenet"}`:  1,
		`prowlarr_indexer_disabled{indexer="X"}`:                                     0,
		`prowlarr_indexer_disabled{indexer="Y"}`:                                     1,
		`prowlarr_indexer_disabled{indexer="Z"}`:                                     0,
		`prowlarr_indexer_disabled{indexer="id:99"}`:                                 1,
		// Two rows named X are summed; the response time is the slower one.
		`prowlarr_indexer_response_time_ms{indexer="X"}`:          300,
		`prowlarr_indexer_queries_total{indexer="X"}`:             15,
		`prowlarr_indexer_grabs_total{indexer="X"}`:               3,
		`prowlarr_indexer_rss_queries_total{indexer="X"}`:         40,
		`prowlarr_indexer_auth_queries_total{indexer="X"}`:        2,
		`prowlarr_indexer_failed_queries_total{indexer="X"}`:      3,
		`prowlarr_indexer_failed_grabs_total{indexer="X"}`:        1,
		`prowlarr_indexer_failed_rss_queries_total{indexer="X"}`:  4,
		`prowlarr_indexer_failed_auth_queries_total{indexer="X"}`: 1,
		`prowlarr_indexer_response_time_ms{indexer="Y"}`:          1031,
		`prowlarr_indexer_rss_queries_total{indexer="Y"}`:         4277,
		`prowlarr_user_agent_queries_total{user_agent="Sonarr"}`:  20900,
		`prowlarr_user_agent_grabs_total{user_agent="Sonarr"}`:    4,
		`prowlarr_user_agent_queries_total{user_agent="Radarr"}`:  100,
		`prowlarr_user_agent_grabs_total{user_agent="Radarr"}`:    1,
	})
	// Prowlarr has no queue, root folders or wanted list.
	expectAbsent(t, got, "arr_queue_total", "arr_rootfolder_free_bytes", "arr_wanted_missing")
}

func TestCollectBazarr(t *testing.T) {
	s, got := collectWith(t, collectBazarr, newFakeAPI(bazarrFixture), "")
	if !s.up || len(s.errs) != 0 {
		t.Fatalf("up=%v errs=%v, want up and no errors", s.up, s.errs)
	}
	expect(t, got, map[string]float64{
		`arr_info{app="test",version="1.6.2"}`: 1,
		`arr_health_issues{app="test"}`:        3,
		`arr_health_issue{app="test",message="Unreachable",source="Sonarr",type=""}`: 2,
		`arr_health_issue{app="test",message="Bad key",source="Radarr",type=""}`:     1,
		`bazarr_missing_subtitles{type="episode"}`:                                  7,
		`bazarr_missing_subtitles{type="movie"}`:                                    2,
		`bazarr_throttled_providers`:                                                1,
		`bazarr_signalr_connected{target="sonarr"}`:                                 1,
		`bazarr_signalr_connected{target="radarr"}`:                                 0,
		`bazarr_history_records{type="episode"}`:                                    37,
		`bazarr_history_records{type="movie"}`:                                      89,
	})
}

// TestCollectorsSurviveFailingEndpoints: when every endpoint fails, each
// request is recorded once, the app is down and nothing half-built leaks
// into the snapshot.
func TestCollectorsSurviveFailingEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fn       collectFunc
		requests int
	}{
		{"sonarr", collectSonarr, 7},
		{"radarr", collectRadarr, 7},
		{"prowlarr", collectProwlarr, 5},
		{"bazarr", collectBazarr, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, got := collectWith(t, tc.fn, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}), "")
			if s.up {
				t.Error("up = true, want false")
			}
			if len(s.errs) != tc.requests {
				t.Errorf("%d errors, want %d: %v", len(s.errs), tc.requests, s.errs)
			}
			if len(got) != 0 {
				t.Errorf("got series %v, want none", keys(got))
			}
		})
	}
}

// TestCollectorsAcceptEmptyReplies: a fresh install returns empty lists and
// zero counts; every summary gauge is still exported as 0.
func TestCollectorsAcceptEmptyReplies(t *testing.T) {
	empty := map[string]string{
		"/system/status": `{}`, "/health": `[]`, "/queue": `{}`, "/rootfolder": `[]`,
		"/wanted/missing": `{}`, "/wanted/cutoff": `{}`, "/series": `[]`, "/movie": `[]`,
		"/indexer": `[]`, "/indexerstatus": `[]`, "/indexerstats": `{}`,
	}
	_, got := collectWith(t, collectSonarr, newFakeAPI(empty), "")
	expect(t, got, map[string]float64{`sonarr_series`: 0, `sonarr_size_on_disk_bytes`: 0, `arr_queue_total{app="test"}`: 0, `arr_info{app="test",version=""}`: 1})
	_, got = collectWith(t, collectRadarr, newFakeAPI(empty), "")
	expect(t, got, map[string]float64{`radarr_movies`: 0})
	_, got = collectWith(t, collectProwlarr, newFakeAPI(empty), "")
	expect(t, got, map[string]float64{`prowlarr_indexers`: 0, `prowlarr_indexers_enabled`: 0})
}

// queueHandler serves a queue of total items, page by page.
func queueHandler(total int, failPage int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		size, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
		if page == failPage {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		n := min(size, total-(page-1)*size)
		recs := make([]string, 0, n)
		for i := 0; i < n; i++ {
			recs = append(recs, `{"size":10,"sizeleft":1,"status":"queued","trackedDownloadStatus":"ok","trackedDownloadState":"downloading"}`)
		}
		fmt.Fprintf(w, `{"page":%d,"pageSize":%d,"totalRecords":%d,"records":[%s]}`, page, size, total, strings.Join(recs, ","))
	}
}

func TestQueuePaging(t *testing.T) {
	f := newFakeAPI(nil)
	f.handle("/queue", queueHandler(300, 0))
	s, got := collectWith(t, collectQueue, f, "")
	if len(s.errs) != 0 {
		t.Fatal(s.errs)
	}
	expect(t, got, map[string]float64{
		`arr_queue_total{app="test"}`:           300,
		`arr_queue_size_bytes{app="test"}`:      3000,
		`arr_queue_remaining_bytes{app="test"}`: 300,
		`arr_queue_items{app="test",download_state="downloading",download_status="ok",status="queued"}`: 300,
	})
	if h := f.hitCount("/queue"); h != 2 {
		t.Errorf("%d queue requests for 300 items, want 2", h)
	}
}

func TestQueuePagingStopsAtLimit(t *testing.T) {
	f := newFakeAPI(nil)
	f.handle("/queue", queueHandler(1_000_000, 0))
	s, got := collectWith(t, collectQueue, f, "")
	if len(s.errs) != 0 {
		t.Fatal(s.errs)
	}
	if h := f.hitCount("/queue"); h != maxQueuePages {
		t.Errorf("%d queue requests, want the %d-page limit", h, maxQueuePages)
	}
	expect(t, got, map[string]float64{`arr_queue_total{app="test"}`: 1_000_000})
}

func TestQueuePageFailureDropsQueueMetrics(t *testing.T) {
	f := newFakeAPI(nil)
	f.handle("/queue", queueHandler(300, 2))
	s, got := collectWith(t, collectQueue, f, "")
	if len(s.errs) != 1 {
		t.Fatalf("errors = %v, want 1", s.errs)
	}
	if len(got) != 0 {
		t.Errorf("partial queue exported: %v", keys(got))
	}
}

func countPrefix(m map[string]float64, name string) int {
	n := 0
	for k := range m {
		if strings.HasPrefix(k, name+"{") {
			n++
		}
	}
	return n
}
