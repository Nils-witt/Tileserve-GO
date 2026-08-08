package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nilswitt.dev/tileserve-go/internal/version"
)

func TestVersionHandler(t *testing.T) {
	origVersion, origCommit := version.Version, version.Commit
	t.Cleanup(func() { version.Version, version.Commit = origVersion, origCommit })

	t.Run("untagged build omits version", func(t *testing.T) {
		version.Version = ""
		version.Commit = "abc1234"

		r := httptest.NewRequest(http.MethodGet, "/version", nil)
		w := httptest.NewRecorder()
		VersionHandler().ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
		var body versionResponse
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if body.Commit != "abc1234" || body.Version != "" {
			t.Fatalf("body = %+v, want commit=abc1234 version=\"\"", body)
		}
	})

	t.Run("tagged build includes version", func(t *testing.T) {
		version.Version = "v1.2.3"
		version.Commit = "abc1234"

		r := httptest.NewRequest(http.MethodGet, "/version", nil)
		w := httptest.NewRecorder()
		VersionHandler().ServeHTTP(w, r)

		var body versionResponse
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if body.Commit != "abc1234" || body.Version != "v1.2.3" {
			t.Fatalf("body = %+v, want commit=abc1234 version=v1.2.3", body)
		}
	})

	t.Run("rejects non-GET methods", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/version", nil)
		w := httptest.NewRecorder()
		VersionHandler().ServeHTTP(w, r)

		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
		}
	})
}
