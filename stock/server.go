package main

import "net/http"

// newMux serves the Prometheus endpoint at /metrics and redirects the bare
// root to it, so opening http://host:port/ in a browser lands on the same
// page Prometheus scrapes. Any other path is a 404.
func newMux(metrics http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/metrics", http.StatusFound)
	})
	return mux
}
