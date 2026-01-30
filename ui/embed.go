// Copyright Contributors to the KubeOpenCode project

package ui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist/*
var staticFiles embed.FS

// Handler returns an http.Handler that serves the embedded UI files.
// The baseURL parameter allows serving the UI from a subpath (e.g., "/kubeopencode").
func Handler(baseURL string) http.Handler {
	// Get the dist subdirectory
	distFS, err := fs.Sub(staticFiles, "dist")
	if err != nil {
		// This should never happen since dist is embedded
		panic(err)
	}

	fileServer := http.FileServer(http.FS(distFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip the base URL if provided
		path := r.URL.Path
		if baseURL != "" && strings.HasPrefix(path, baseURL) {
			path = strings.TrimPrefix(path, baseURL)
			if path == "" {
				path = "/"
			}
		}

		// Try to serve the file
		// For SPA routing, if the file doesn't exist and it's not an API request,
		// serve index.html
		if path != "/" && !strings.HasPrefix(path, "/api/") {
			// Check if the file exists
			if _, err := fs.Stat(distFS, strings.TrimPrefix(path, "/")); err != nil {
				// File doesn't exist, serve index.html for SPA routing
				path = "/"
				r.URL.Path = "/"
			}
		}

		fileServer.ServeHTTP(w, r)
	})
}
