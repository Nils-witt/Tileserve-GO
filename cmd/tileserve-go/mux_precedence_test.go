package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestMuxPrecedence locks in the one part of the direct-mux-registration
// refactor whose correctness depends on Go's stdlib http.ServeMux
// precedence rules rather than anything this codebase controls directly:
// that a literal sub-resource pattern (bounds/archive/download/geo-objects)
// always wins over the wildcard tile-file pattern for the four reserved
// path segments, and that both the exact and wildcard tile-file
// registrations are needed to preserve a plain 404 (rather than a
// redirect) for a bare "/maps/{id}/version/{version}" request with no
// trailing file. It builds a minimal mux with the exact same route
// patterns registered in run(), wired to dummy handlers that just report
// which one matched, and doesn't require a database or any other
// dependency real run() needs.
func TestMuxPrecedence(t *testing.T) {
	t.Parallel()

	hit := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Matched", name)
			w.WriteHeader(http.StatusOK)
		}
	}

	mux := http.NewServeMux()
	mux.Handle("GET /maps/{id}/version/{version}/bounds", hit("bounds"))
	mux.Handle("GET /maps/{id}/version/{version}/archive", hit("archive"))
	mux.Handle("GET /maps/{id}/version/{version}/download", hit("download"))
	mux.Handle("GET /maps/{id}/version/{version}/geo-objects", hit("geo-objects-list"))
	mux.Handle("GET /maps/{id}/version/{version}/geo-objects/{objectId}", hit("geo-objects-item"))
	mux.Handle("/maps/{id}/version/{version}", hit("tile-file-exact"))
	mux.Handle("/maps/{id}/version/{version}/{filepath...}", hit("tile-file-wildcard"))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := srv.Client()
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantMatch  string // empty means "don't check X-Matched" (e.g. a redirect/404 with no handler run)
	}{
		{"bounds literal wins over wildcard", "/maps/abc/version/3/bounds", http.StatusOK, "bounds"},
		{"archive literal wins over wildcard", "/maps/abc/version/3/archive", http.StatusOK, "archive"},
		{"download literal wins over wildcard", "/maps/abc/version/3/download", http.StatusOK, "download"},
		{"geo-objects collection literal wins over wildcard", "/maps/abc/version/3/geo-objects", http.StatusOK, "geo-objects-list"},
		{"geo-objects item literal wins over wildcard", "/maps/abc/version/3/geo-objects/obj-1", http.StatusOK, "geo-objects-item"},
		{"real tile file falls through to wildcard", "/maps/abc/version/3/0/0/0.png", http.StatusOK, "tile-file-wildcard"},
		{"bare version (no trailing file) hits the exact pattern, not a redirect", "/maps/abc/version/3", http.StatusOK, "tile-file-exact"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+tt.path, nil)
			if err != nil {
				t.Fatalf("NewRequest %s: %v", tt.path, err)
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("GET %s: status = %d, want %d", tt.path, resp.StatusCode, tt.wantStatus)
			}

			if tt.wantMatch != "" {
				if got := resp.Header.Get("X-Matched"); got != tt.wantMatch {
					t.Errorf("GET %s: matched handler = %q, want %q", tt.path, got, tt.wantMatch)
				}
			}
		})
	}
}

// TestMuxPrecedenceSPACatchAll locks in the frontend-embedding refactor's
// equivalent assumption: registering the SPA handler at the least-specific
// possible pattern, bare "/" (no method), never shadows a more specific API
// route registered anywhere else on the mux (registration order shouldn't
// matter, but this registers "/" first specifically to prove that), and
// still catches every path -- "/", "/login", "/ui", a deep "/ui/..." link,
// and any other unmatched path -- that no more specific route claims. "/"
// must stay unpinned rather than "GET /": pairing "GET /" against another
// route that is itself registered without a method (like /healthz here, or
// /refresh and the OIDC routes in the real mux) is an ambiguous conflict
// ServeMux rejects at startup -- "GET /" is more specific on method (GET
// only vs. every method) but less specific on path ("/" vs. "/healthz"),
// and neither pattern dominates the other. Bare "/" avoids this because it
// is the strict superset of every other pattern on both axes.
func TestMuxPrecedenceSPACatchAll(t *testing.T) {
	t.Parallel()

	const spaMatch = "spa"

	hit := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Matched", name)
			w.WriteHeader(http.StatusOK)
		}
	}

	mux := http.NewServeMux()
	mux.Handle("/", hit(spaMatch))
	mux.Handle("/healthz", hit("healthz"))
	mux.Handle("GET /maps", hit("maps-list"))
	mux.Handle("GET /maps/{id}", hit("maps-get"))
	mux.Handle("POST /login", hit("login-api"))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tests := []struct {
		name      string
		method    string
		path      string
		wantMatch string
	}{
		{"root serves the SPA", http.MethodGet, "/", spaMatch},
		{"login page serves the SPA", http.MethodGet, "/login", spaMatch},
		{"admin app serves the SPA", http.MethodGet, "/ui", spaMatch},
		{"a deep admin app link serves the SPA", http.MethodGet, "/ui/some/deep/link", spaMatch},
		{"an unmatched path still serves the SPA", http.MethodGet, "/whatever", spaMatch},
		{"a specific API route wins over the catch-all", http.MethodGet, "/maps", "maps-list"},
		{"a specific parameterized API route wins over the catch-all", http.MethodGet, "/maps/abc", "maps-get"},
		{"a different method on the same path still hits its own route", http.MethodPost, "/login", "login-api"},
		{"an unmethoded route still wins over the catch-all", http.MethodGet, "/healthz", "healthz"},
		{"an unmethoded route matches any method, still beating the catch-all", http.MethodPost, "/healthz", "healthz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(t.Context(), tt.method, srv.URL+tt.path, nil)
			if err != nil {
				t.Fatalf("NewRequest %s %s: %v", tt.method, tt.path, err)
			}

			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("%s %s: %v", tt.method, tt.path, err)
			}
			defer func() { _ = resp.Body.Close() }()

			if got := resp.Header.Get("X-Matched"); got != tt.wantMatch {
				t.Errorf("%s %s: matched handler = %q, want %q", tt.method, tt.path, got, tt.wantMatch)
			}
		})
	}
}
