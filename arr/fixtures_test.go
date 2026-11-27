package main

// Trimmed API replies, shaped like the real ones (Sonarr 4, Radarr 7,
// Prowlarr 3, Bazarr 1.6). Keys are paths below the app's API root.

var sonarrFixture = map[string]string{
	"/system/status": `{"appName":"Sonarr","version":"4.0.20.3014","branch":"main"}`,
	"/health": `[
		{"source":"IndexerStatusCheck","type":"warning","message":"Indexers unavailable: A","wikiUrl":"x"},
		{"source":"IndexerStatusCheck","type":"warning","message":"Indexers unavailable: A","wikiUrl":"x"},
		{"source":"RootFolderCheck","type":"error","message":"Missing root folder: /tv","wikiUrl":"y"}
	]`,
	"/queue": `{"page":1,"pageSize":250,"totalRecords":3,"records":[
		{"size":1000,"sizeleft":400,"status":"downloading","trackedDownloadStatus":"ok","trackedDownloadState":"downloading"},
		{"size":2000,"sizeleft":0,"status":"completed","trackedDownloadStatus":"warning","trackedDownloadState":"importBlocked"},
		{"size":3000,"sizeleft":1000,"status":"downloading","trackedDownloadStatus":"ok","trackedDownloadState":"downloading"}
	]}`,
	"/rootfolder": `[
		{"path":"/series","accessible":true,"freeSpace":2059317805056,"unmappedFolders":[],"id":1},
		{"path":"/series","accessible":true,"freeSpace":2059317805056,"id":2},
		{"path":"/nfs","accessible":false,"freeSpace":0,"id":3}
	]`,
	"/wanted/missing": `{"page":1,"pageSize":1,"totalRecords":5,"records":[]}`,
	"/wanted/cutoff":  `{"page":1,"pageSize":1,"totalRecords":2,"records":[]}`,
	"/series": `[
		{"title":"A","status":"continuing","monitored":true,"statistics":{"seasonCount":3,"episodeFileCount":22,"episodeCount":22,"totalEpisodeCount":32,"sizeOnDisk":50000,"percentOfEpisodes":100}},
		{"title":"B","status":"ended","monitored":false,"statistics":{"seasonCount":1,"episodeFileCount":4,"episodeCount":10,"totalEpisodeCount":10,"sizeOnDisk":7000,"percentOfEpisodes":40}},
		{"title":"C","status":"continuing","monitored":true,"statistics":{"seasonCount":2,"episodeFileCount":0,"episodeCount":0,"totalEpisodeCount":0,"sizeOnDisk":0,"percentOfEpisodes":0}}
	]`,
}

var radarrFixture = map[string]string{
	"/system/status":  `{"appName":"Radarr","version":"7.0.3.10918"}`,
	"/health":         `[]`,
	"/queue":          `{"page":1,"pageSize":250,"totalRecords":0,"records":[]}`,
	"/rootfolder":     `[{"path":"/movies","accessible":true,"freeSpace":123}]`,
	"/wanted/missing": `{"totalRecords":1}`,
	"/wanted/cutoff":  `{"totalRecords":0}`,
	"/movie": `[
		{"title":"A","status":"released","monitored":true,"hasFile":true,"sizeOnDisk":9000,"movieFile":{"size":9000,"quality":{"quality":{"id":7,"name":"Bluray-1080p"}}}},
		{"title":"B","status":"released","monitored":false,"hasFile":true,"sizeOnDisk":4000,"movieFile":{"size":4000,"quality":{"quality":{"name":"Bluray-1080p"}}}},
		{"title":"C","status":"inCinemas","monitored":true,"hasFile":true,"sizeOnDisk":1000,"movieFile":{"size":1000,"quality":{"quality":{"name":"WEBDL-2160p"}}}},
		{"title":"D","status":"announced","monitored":true,"hasFile":false,"sizeOnDisk":0},
		{"title":"E","status":"released","monitored":true,"hasFile":true,"sizeOnDisk":0}
	]`,
}

var prowlarrFixture = map[string]string{
	"/system/status": `{"appName":"Prowlarr","version":"3.1.0.5799"}`,
	"/health":        `[{"source":"IndexerStatusCheck","type":"warning","message":"Indexers unavailable due to failures: Y"}]`,
	"/indexer": `[
		{"id":1,"name":"X","enable":true,"protocol":"torrent","privacy":"public","fields":[{"name":"baseUrl","value":"https://x"}]},
		{"id":2,"name":"Y","enable":false,"protocol":"torrent","privacy":"private"},
		{"id":3,"name":"X","enable":true,"protocol":"torrent","privacy":"public"},
		{"id":4,"name":"Z","enable":true,"protocol":"usenet","privacy":"private"}
	]`,
	"/indexerstatus": `[
		{"indexerId":2,"disabledTill":"2099-01-01T00:00:00Z","initialFailure":"2026-01-01T00:00:00Z"},
		{"indexerId":4,"disabledTill":"2000-01-01T00:00:00Z"},
		{"indexerId":1},
		{"indexerId":99,"disabledTill":"2099-01-01T00:00:00Z"}
	]`,
	"/indexerstats": `{"indexers":[
		{"indexerId":1,"indexerName":"X","averageResponseTime":100,"numberOfQueries":10,"numberOfGrabs":2,"numberOfRssQueries":30,"numberOfAuthQueries":1,"numberOfFailedQueries":1,"numberOfFailedGrabs":0,"numberOfFailedRssQueries":3,"numberOfFailedAuthQueries":0},
		{"indexerId":3,"indexerName":"X","averageResponseTime":300,"numberOfQueries":5,"numberOfGrabs":1,"numberOfRssQueries":10,"numberOfAuthQueries":1,"numberOfFailedQueries":2,"numberOfFailedGrabs":1,"numberOfFailedRssQueries":1,"numberOfFailedAuthQueries":1},
		{"indexerId":2,"indexerName":"Y","averageResponseTime":1031,"numberOfQueries":12,"numberOfGrabs":1,"numberOfRssQueries":4277}
	],"userAgents":[
		{"userAgent":"Sonarr","numberOfQueries":20899,"numberOfGrabs":3},
		{"userAgent":"Radarr","numberOfQueries":100,"numberOfGrabs":1},
		{"userAgent":"Sonarr","numberOfQueries":1,"numberOfGrabs":1}
	],"hosts":[{"host":"172.23.0.1","numberOfQueries":32906,"numberOfGrabs":11}]}`,
}

var bazarrFixture = map[string]string{
	"/system/status":    `{"data":{"bazarr_version":"1.6.2","python_version":"3.12.16","start_time":1795726213.694853}}`,
	"/system/health":    `{"data":[{"object":"Sonarr","issue":"Unreachable"},{"object":"Sonarr","issue":"Unreachable"},{"object":"Radarr","issue":"Bad key"}]}`,
	"/badges":           `{"episodes":7,"movies":2,"providers":1,"status":0,"sonarr_signalr":"LIVE","radarr_signalr":"","announcements":1}`,
	"/episodes/history": `{"data":[{"provider":"embeddedsubtitles","score":"100.0%"}],"total":37}`,
	"/movies/history":   `{"data":[],"total":89}`,
}

// prefixed returns a copy of fixture with every path under apiPath, for
// tests that go through the real per-app API roots.
func prefixed(apiPath string, fixture map[string]string) map[string]string {
	out := make(map[string]string, len(fixture))
	for k, v := range fixture {
		out[apiPath+k] = v
	}
	return out
}
