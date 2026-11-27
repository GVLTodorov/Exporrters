package main

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// --- Sonarr (API v3) ---

func collectSonarr(ctx context.Context, c *apiClient, s *sink) {
	collectStatus(ctx, c, s)
	collectHealth(ctx, c, s)
	collectQueue(ctx, c, s)
	collectRootFolders(ctx, c, s)
	collectWanted(ctx, c, s)

	var series []struct {
		Monitored  bool   `json:"monitored"`
		Status     string `json:"status"`
		Statistics struct {
			SeasonCount       int     `json:"seasonCount"`
			EpisodeFileCount  int     `json:"episodeFileCount"`
			EpisodeCount      int     `json:"episodeCount"`
			TotalEpisodeCount int     `json:"totalEpisodeCount"`
			SizeOnDisk        int64   `json:"sizeOnDisk"`
			PercentOfEpisodes float64 `json:"percentOfEpisodes"`
		} `json:"statistics"`
	}
	if err := c.get(ctx, "/series", nil, &series); err != nil {
		s.fail(err)
		return
	}
	var monitored, complete, seasons, episodes, all, files int
	var size int64
	status := map[string]int{}
	for _, x := range series {
		if x.Monitored {
			monitored++
		}
		if x.Statistics.PercentOfEpisodes >= 100 {
			complete++
		}
		status[x.Status]++
		seasons += x.Statistics.SeasonCount
		episodes += x.Statistics.EpisodeCount
		all += x.Statistics.TotalEpisodeCount
		files += x.Statistics.EpisodeFileCount
		size += x.Statistics.SizeOnDisk
	}
	s.gauge(descSonarrSeries, float64(len(series)))
	s.gauge(descSonarrSeriesMonitored, float64(monitored))
	s.gauge(descSonarrSeriesComplete, float64(complete))
	emitCounts(status, func(k string, n float64) { s.gauge(descSonarrSeriesStatus, n, k) })
	s.gauge(descSonarrSeasons, float64(seasons))
	s.gauge(descSonarrEpisodes, float64(episodes))
	s.gauge(descSonarrEpisodesAll, float64(all))
	s.gauge(descSonarrEpisodeFiles, float64(files))
	s.gauge(descSonarrSize, float64(size))
}

// --- Radarr (API v3) ---

func collectRadarr(ctx context.Context, c *apiClient, s *sink) {
	collectStatus(ctx, c, s)
	collectHealth(ctx, c, s)
	collectQueue(ctx, c, s)
	collectRootFolders(ctx, c, s)
	collectWanted(ctx, c, s)

	var movies []struct {
		Monitored  bool   `json:"monitored"`
		HasFile    bool   `json:"hasFile"`
		Status     string `json:"status"`
		SizeOnDisk int64  `json:"sizeOnDisk"`
		MovieFile  *struct {
			Quality struct {
				Quality struct {
					Name string `json:"name"`
				} `json:"quality"`
			} `json:"quality"`
		} `json:"movieFile"`
	}
	// excludeLocalCovers drops the image list, roughly halving the reply.
	if err := c.get(ctx, "/movie", url.Values{"excludeLocalCovers": {"true"}}, &movies); err != nil {
		s.fail(err)
		return
	}
	var monitored, downloaded int
	var size int64
	status := map[string]int{}
	quality := map[string]int{}
	for _, m := range movies {
		if m.Monitored {
			monitored++
		}
		if m.HasFile {
			downloaded++
		}
		if m.HasFile && m.MovieFile != nil {
			quality[m.MovieFile.Quality.Quality.Name]++
		}
		status[m.Status]++
		size += m.SizeOnDisk
	}
	s.gauge(descRadarrMovies, float64(len(movies)))
	s.gauge(descRadarrMonitored, float64(monitored))
	s.gauge(descRadarrDownloaded, float64(downloaded))
	s.gauge(descRadarrSize, float64(size))
	emitCounts(status, func(k string, n float64) { s.gauge(descRadarrStatus, n, k) })
	emitCounts(quality, func(k string, n float64) { s.gauge(descRadarrQuality, n, k) })
}

// --- Prowlarr (API v1) ---

// indexerStats is one row of Prowlarr's /indexerstats reply. Without a date
// range Prowlarr returns all-time totals from its history, so the counts
// only grow (until history is cleared, which Prometheus treats as a counter
// reset).
type indexerStats struct {
	Name                      string `json:"indexerName"`
	AverageResponseTime       int    `json:"averageResponseTime"`
	NumberOfQueries           int    `json:"numberOfQueries"`
	NumberOfGrabs             int    `json:"numberOfGrabs"`
	NumberOfRssQueries        int    `json:"numberOfRssQueries"`
	NumberOfAuthQueries       int    `json:"numberOfAuthQueries"`
	NumberOfFailedQueries     int    `json:"numberOfFailedQueries"`
	NumberOfFailedGrabs       int    `json:"numberOfFailedGrabs"`
	NumberOfFailedRssQueries  int    `json:"numberOfFailedRssQueries"`
	NumberOfFailedAuthQueries int    `json:"numberOfFailedAuthQueries"`
}

func collectProwlarr(ctx context.Context, c *apiClient, s *sink) {
	collectStatus(ctx, c, s)
	collectHealth(ctx, c, s)

	var indexers []struct {
		ID       int    `json:"id"`
		Name     string `json:"name"`
		Enable   bool   `json:"enable"`
		Protocol string `json:"protocol"`
		Privacy  string `json:"privacy"`
	}
	names := map[int]string{}
	if err := c.get(ctx, "/indexer", nil, &indexers); err != nil {
		s.fail(err)
	} else {
		enabled := 0
		seen := map[string]bool{}
		for _, ix := range indexers {
			names[ix.ID] = ix.Name
			if ix.Enable {
				enabled++
			}
			if seen[ix.Name] {
				continue
			}
			seen[ix.Name] = true
			s.gauge(descProwlarrIndexerEnabled, boolFloat(ix.Enable), ix.Name, ix.Protocol, ix.Privacy)
		}
		s.gauge(descProwlarrIndexers, float64(len(indexers)))
		s.gauge(descProwlarrIndexersEnabled, float64(enabled))
	}

	// Indexers Prowlarr has backed off from after repeated failures.
	var statuses []struct {
		IndexerID    int        `json:"indexerId"`
		DisabledTill *time.Time `json:"disabledTill"`
	}
	if err := c.get(ctx, "/indexerstatus", nil, &statuses); err != nil {
		s.fail(err)
	} else {
		now := time.Now()
		disabled := map[string]bool{}
		for _, st := range statuses {
			name, ok := names[st.IndexerID]
			if !ok {
				name = "id:" + strconv.Itoa(st.IndexerID)
			}
			disabled[name] = disabled[name] || (st.DisabledTill != nil && st.DisabledTill.After(now))
		}
		for name, d := range disabled {
			s.gauge(descProwlarrIndexerDisabled, boolFloat(d), name)
		}
	}

	var stats struct {
		Indexers   []indexerStats `json:"indexers"`
		UserAgents []struct {
			UserAgent       string `json:"userAgent"`
			NumberOfQueries int    `json:"numberOfQueries"`
			NumberOfGrabs   int    `json:"numberOfGrabs"`
		} `json:"userAgents"`
	}
	if err := c.get(ctx, "/indexerstats", nil, &stats); err != nil {
		s.fail(err)
		return
	}
	// Sum rows that share a name so each label set appears once.
	byName := map[string]*indexerStats{}
	for i := range stats.Indexers {
		row := stats.Indexers[i]
		acc, ok := byName[row.Name]
		if !ok {
			byName[row.Name] = &row
			continue
		}
		acc.AverageResponseTime = max(acc.AverageResponseTime, row.AverageResponseTime)
		acc.NumberOfQueries += row.NumberOfQueries
		acc.NumberOfGrabs += row.NumberOfGrabs
		acc.NumberOfRssQueries += row.NumberOfRssQueries
		acc.NumberOfAuthQueries += row.NumberOfAuthQueries
		acc.NumberOfFailedQueries += row.NumberOfFailedQueries
		acc.NumberOfFailedGrabs += row.NumberOfFailedGrabs
		acc.NumberOfFailedRssQueries += row.NumberOfFailedRssQueries
		acc.NumberOfFailedAuthQueries += row.NumberOfFailedAuthQueries
	}
	for name, st := range byName {
		s.gauge(descProwlarrResponseTime, float64(st.AverageResponseTime), name)
		s.counter(descProwlarrQueries, float64(st.NumberOfQueries), name)
		s.counter(descProwlarrGrabs, float64(st.NumberOfGrabs), name)
		s.counter(descProwlarrRSS, float64(st.NumberOfRssQueries), name)
		s.counter(descProwlarrAuth, float64(st.NumberOfAuthQueries), name)
		s.counter(descProwlarrFailedQueries, float64(st.NumberOfFailedQueries), name)
		s.counter(descProwlarrFailedGrabs, float64(st.NumberOfFailedGrabs), name)
		s.counter(descProwlarrFailedRSS, float64(st.NumberOfFailedRssQueries), name)
		s.counter(descProwlarrFailedAuth, float64(st.NumberOfFailedAuthQueries), name)
	}
	uaQueries, uaGrabs := map[string]int{}, map[string]int{}
	for _, ua := range stats.UserAgents {
		uaQueries[ua.UserAgent] += ua.NumberOfQueries
		uaGrabs[ua.UserAgent] += ua.NumberOfGrabs
	}
	for ua, n := range uaQueries {
		s.counter(descProwlarrUAQueries, float64(n), ua)
		s.counter(descProwlarrUAGrabs, float64(uaGrabs[ua]), ua)
	}
}

// --- Bazarr (/api) ---

func collectBazarr(ctx context.Context, c *apiClient, s *sink) {
	var st struct {
		Data struct {
			Version string `json:"bazarr_version"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/system/status", nil, &st); err != nil {
		s.fail(err)
	} else {
		s.up = true
		s.shared(descInfo, 1, st.Data.Version)
	}

	var health struct {
		Data []struct {
			Object string `json:"object"`
			Issue  string `json:"issue"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/system/health", nil, &health); err != nil {
		s.fail(err)
	} else {
		counts := map[[2]string]int{}
		for _, h := range health.Data {
			counts[[2]string{h.Object, h.Issue}]++
		}
		s.shared(descHealthIssues, float64(len(health.Data)))
		emitCounts(counts, func(k [2]string, n float64) { s.shared(descHealthIssue, n, k[0], "", k[1]) })
	}

	var badges struct {
		Episodes      int    `json:"episodes"`
		Movies        int    `json:"movies"`
		Providers     int    `json:"providers"`
		SonarrSignalr string `json:"sonarr_signalr"`
		RadarrSignalr string `json:"radarr_signalr"`
	}
	if err := c.get(ctx, "/badges", nil, &badges); err != nil {
		s.fail(err)
	} else {
		s.gauge(descBazarrMissing, float64(badges.Episodes), "episode")
		s.gauge(descBazarrMissing, float64(badges.Movies), "movie")
		s.gauge(descBazarrThrottled, float64(badges.Providers))
		s.gauge(descBazarrSignalr, boolFloat(badges.SonarrSignalr == "LIVE"), "sonarr")
		s.gauge(descBazarrSignalr, boolFloat(badges.RadarrSignalr == "LIVE"), "radarr")
	}

	for _, kind := range []string{"episode", "movie"} {
		var h struct {
			Total int `json:"total"`
		}
		if err := c.get(ctx, "/"+kind+"s/history", url.Values{"length": {"1"}}, &h); err != nil {
			s.fail(err)
			continue
		}
		s.gauge(descBazarrHistory, float64(h.Total), kind)
	}
}
