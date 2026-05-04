package app

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:spa
var spaFS embed.FS

// embeddedSPA returns an http.Handler that serves the embedded Svelte build,
// or nil if the build hasn't been produced yet (server falls back to the
// placeholder page).
func embeddedSPA() http.Handler {
	sub, err := fs.Sub(spaFS, "spa")
	if err != nil {
		return nil
	}
	// Probe: if index.html is missing the build hasn't run.
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// SPA fallback: serve index.html for paths without a file extension
		// (and not under /api/ or /ws).
		p := r.URL.Path
		if p != "/" && !strings.Contains(p[1:], ".") {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})
}
