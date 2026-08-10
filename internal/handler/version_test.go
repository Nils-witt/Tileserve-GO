package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nilswitt.dev/tileserve-go/internal/version"
)

// testCommit is the fake build commit used throughout this file's assertions.
const testCommit = "abc1234"

//nolint:paralleltest // subtests mutate the shared package-level version.Version/version.Commit and must run sequentially
func TestVersionHandler(t *testing.T) {
	origVersion, origCommit := version.Version, version.Commit

	t.Cleanup(func() { version.Version, version.Commit = origVersion, origCommit })

	t.Run("untagged build omits version", func(t *testing.T) {
		version.Version = ""
		version.Commit = testCommit

		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/version", nil)
		w := httptest.NewRecorder()
		VersionHandler().ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}

		var body versionResponse
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		if body.Commit != testCommit || body.Version != "" {
			t.Fatalf("body = %+v, want commit=abc1234 version=\"\"", body)
		}
	})

	t.Run("tagged build includes version", func(t *testing.T) {
		version.Version = "v1.2.3"
		version.Commit = testCommit

		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/version", nil)
		w := httptest.NewRecorder()
		VersionHandler().ServeHTTP(w, r)

		var body versionResponse
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		if body.Commit != testCommit || body.Version != "v1.2.3" {
			t.Fatalf("body = %+v, want commit=abc1234 version=v1.2.3", body)
		}
	})

	t.Run("rejects non-GET methods", func(t *testing.T) {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/version", nil)
		w := httptest.NewRecorder()
		VersionHandler().ServeHTTP(w, r)

		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
		}
	})
}
