// Package ui serves the self-contained management UI's static assets.
package ui

import (
	_ "embed"
	"net/http"
)

//go:embed ui.html
var uiPage []byte

//go:embed ui.js
var uiScript []byte

// ServeUIHandler serves the self-contained management UI. The page itself is
// public (it must be reachable before a token exists); every action it takes
// calls the normal authenticated JSON API with a token obtained via /login.
func ServeUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(uiPage)
	}
}

// ServeUIScriptHandler serves the UI's script at /ui/app.js, since the page
// references it with a path relative to /ui/.
func ServeUIScriptHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(uiScript)
	}
}
