package api

import (
	"io/fs"
	"net/http"
	"strings"
)

// serveDashboardHandler returns an http.Handler that serves the embedded
// SvelteKit SPA from the given filesystem. Non-file paths fall back to
// index.html so client-side routing (/agents/:id) works. If dashFS is nil,
// the returned handler serves 503 (dashboard not built into this binary).
func serveDashboardHandler(dashFS fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if dashFS == nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("dashboard not built into this binary (build with the dashboard assets to enable)\n"))
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		// Try the exact asset first (e.g. /_app/immutable/...).
		if f, err := dashFS.Open(p); err == nil {
			f.Close()
			http.ServeFileFS(w, r, dashFS, p)
			return
		}
		// SPA fallback: unknown client-side routes render index.html.
		if f, err := dashFS.Open("index.html"); err == nil {
			f.Close()
			http.ServeFileFS(w, r, dashFS, "index.html")
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
}
