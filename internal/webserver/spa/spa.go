// Package spa serves a Vite-built single-page app out of an embedded
// filesystem: a real file (a JS/CSS chunk, favicon, etc.) is served
// directly, and any other path falls back to index.html so the app's own
// client-side router (react-router) can take over — including a deep link
// like /ui/ or a browser refresh on /login.
package spa

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

// Handler builds an http.Handler serving the SPA built into distFS at the
// "dist" subdirectory (see frontend/embed.go).
func Handler(distFS embed.FS) (http.Handler, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, fmt.Errorf("sub dist: %w", err)
	}

	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return nil, fmt.Errorf("read dist/index.html: %w", err)
	}

	fileServer := http.FileServerFS(sub)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestedFileExists(sub, r.URL.Path) {
			fileServer.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	}), nil
}

func requestedFileExists(sub fs.FS, urlPath string) bool {
	name := strings.TrimPrefix(urlPath, "/")
	if name == "" {
		return false
	}

	info, err := fs.Stat(sub, name)
	if err != nil {
		return false
	}

	return !info.IsDir()
}
