package handler

import (
	_ "embed"
	"net/http"
)

//go:embed ui.html
var uiPage []byte

//go:embed ui.js
var uiScript []byte

// UIHandler serves the self-contained management UI. The page itself is
// public (it must be reachable before a token exists); every action it takes
// calls the normal authenticated JSON API with a token obtained via /login.
// It also serves the UI's script at /ui/app.js, since the page references it
// with a path relative to /ui/.
func UIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if r.URL.Path == "/ui/app.js" {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write(uiScript)

			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(uiPage)
	}
}
