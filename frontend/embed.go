// Package frontend embeds the built Vite SPA (see frontend/dist, produced by
// `npm run build`) for the Go server to serve.
package frontend

import "embed"

// DistFS holds the Vite build output (frontend/dist), produced by
// `npm run build`.
//
//go:embed all:dist
var DistFS embed.FS
