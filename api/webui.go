package api

import (
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
)

// webUIHandler returns an http.Handler that serves the embedded web UI.
// It falls back to index.html for SPA client-side routing.
func webUIHandler(webFS fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(webFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// Try exact file
		if _, err := fs.Stat(webFS, path); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// Try directory index
		indexPath := filepath.Join(path, "index.html")
		if _, err := fs.Stat(webFS, indexPath); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve root index.html
		data, err := fs.ReadFile(webFS, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})
}
