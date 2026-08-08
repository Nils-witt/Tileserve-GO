package handler

import (
	"net/http"

	"nilswitt.dev/tileserve-go/internal/version"
)

type versionResponse struct {
	Commit  string `json:"commit"`
	Version string `json:"version,omitempty"`
}

// VersionHandler returns build metadata (git commit, and tag/version if the
// build was made from a tagged release) as JSON. Public, unauthenticated —
// the UI footer fetches it without a token.
func VersionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, versionResponse{
			Commit:  version.Commit,
			Version: version.Version,
		})
	}
}
