package main

import (
	"context"
	"net/url"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

// collectFunc fetches one application's API and adds its metrics to s.
// Failed requests are recorded with s.fail and skipped, so one broken
// endpoint costs only its own metrics.
type collectFunc func(ctx context.Context, c *apiClient, s *sink)

// Endpoints shared by Sonarr, Radarr and Prowlarr (the Servarr family).

func collectStatus(ctx context.Context, c *apiClient, s *sink) {
	var st struct {
		Version string `json:"version"`
	}
	if err := c.get(ctx, "/system/status", nil, &st); err != nil {
		s.fail(err)
		return
	}
	s.up = true
	s.shared(descInfo, 1, st.Version)
}

func collectHealth(ctx context.Context, c *apiClient, s *sink) {
	var issues []struct {
		Source  string `json:"source"`
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if err := c.get(ctx, "/health", nil, &issues); err != nil {
		s.fail(err)
		return
	}
	counts := map[[3]string]int{}
	for _, i := range issues {
		counts[[3]string{i.Source, i.Type, i.Message}]++
	}
	s.shared(descHealthIssues, float64(len(issues)))
	emitCounts(counts, func(k [3]string, n float64) { s.shared(descHealthIssue, n, k[0], k[1], k[2]) })
}

// queuePageSize and maxQueuePages bound the queue walk to 10,000 items.
const (
	queuePageSize = 250
	maxQueuePages = 40
)

func collectQueue(ctx context.Context, c *apiClient, s *sink) {
	counts := map[[3]string]int{}
	var total int
	var size, left float64
	for page := 1; page <= maxQueuePages; page++ {
		var p struct {
			TotalRecords int `json:"totalRecords"`
			Records      []struct {
				Size                  float64 `json:"size"`
				SizeLeft              float64 `json:"sizeleft"`
				Status                string  `json:"status"`
				TrackedDownloadStatus string  `json:"trackedDownloadStatus"`
				TrackedDownloadState  string  `json:"trackedDownloadState"`
			} `json:"records"`
		}
		q := url.Values{"page": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(queuePageSize)}}
		if err := c.get(ctx, "/queue", q, &p); err != nil {
			s.fail(err)
			return
		}
		total = p.TotalRecords
		for _, r := range p.Records {
			counts[[3]string{r.Status, r.TrackedDownloadStatus, r.TrackedDownloadState}]++
			size += r.Size
			left += r.SizeLeft
		}
		if len(p.Records) < queuePageSize || page*queuePageSize >= p.TotalRecords {
			break
		}
	}
	s.shared(descQueueTotal, float64(total))
	s.shared(descQueueSize, size)
	s.shared(descQueueRemaining, left)
	emitCounts(counts, func(k [3]string, n float64) { s.shared(descQueueItems, n, k[0], k[1], k[2]) })
}

func collectRootFolders(ctx context.Context, c *apiClient, s *sink) {
	var folders []struct {
		Path       string `json:"path"`
		Accessible bool   `json:"accessible"`
		FreeSpace  int64  `json:"freeSpace"`
	}
	if err := c.get(ctx, "/rootfolder", nil, &folders); err != nil {
		s.fail(err)
		return
	}
	seen := map[string]bool{}
	for _, f := range folders {
		if seen[f.Path] {
			continue
		}
		seen[f.Path] = true
		s.shared(descRootFree, float64(f.FreeSpace), f.Path)
		s.shared(descRootAccessible, boolFloat(f.Accessible), f.Path)
	}
}

// collectWanted reads only totalRecords from the paged wanted endpoints.
func collectWanted(ctx context.Context, c *apiClient, s *sink) {
	for _, w := range []struct {
		path string
		desc *prometheus.Desc
	}{
		{"/wanted/missing", descWantedMissing},
		{"/wanted/cutoff", descWantedCutoff},
	} {
		var p struct {
			TotalRecords int `json:"totalRecords"`
		}
		if err := c.get(ctx, w.path, url.Values{"pageSize": {"1"}}, &p); err != nil {
			s.fail(err)
			continue
		}
		s.shared(w.desc, float64(p.TotalRecords))
	}
}
