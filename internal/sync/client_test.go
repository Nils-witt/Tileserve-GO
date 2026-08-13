package sync

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"

	"nilswitt.dev/tileserve-go/internal/store"
)

// requireAPIKey wraps handler, rejecting any request not bearing
// "Bearer "+wantKey, mimicking the remote server's own auth gate closely
// enough to exercise Client's request-building.
func requireAPIKey(t *testing.T, wantKey string, handler http.HandlerFunc) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+wantKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		handler(w, r)
	}
}

func TestClientListMaps(t *testing.T) {
	t.Parallel()

	mapID := uuid.New()
	want := []store.MapRecord{{UUID: mapID, Name: "test-map", CurrentVersion: "3"}}

	mux := http.NewServeMux()
	mux.HandleFunc("/maps", requireAPIKey(t, "tsk_test-key", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", r.Method)
		}

		_ = json.NewEncoder(w).Encode(want)
	}))

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewClient(srv.URL, "tsk_test-key")

	got, err := client.ListMaps(t.Context())
	if err != nil {
		t.Fatalf("ListMaps: %v", err)
	}

	if len(got) != 1 || got[0].UUID != mapID || got[0].CurrentVersion != "3" {
		t.Fatalf("ListMaps() = %+v, want %+v", got, want)
	}
}

func TestClientListMapsRejectsBadStatus(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/maps", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewClient(srv.URL, "tsk_wrong-key")

	if _, err := client.ListMaps(t.Context()); err == nil {
		t.Fatal("ListMaps() with a rejected request should return an error")
	}
}

func TestClientDownloadArchive(t *testing.T) {
	t.Parallel()

	mapID := uuid.New()
	body := []byte("fake-zip-bytes")

	mux := http.NewServeMux()
	mux.HandleFunc("/maps/"+mapID.String()+"/version/3/archive", requireAPIKey(t, "tsk_test-key", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewClient(srv.URL, "tsk_test-key")

	tmpPath, err := client.DownloadArchive(t.Context(), mapID, "3")
	if err != nil {
		t.Fatalf("DownloadArchive: %v", err)
	}
	defer func() { _ = os.Remove(tmpPath) }()

	got, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("read downloaded archive: %v", err)
	}

	if string(got) != string(body) {
		t.Fatalf("downloaded archive = %q, want %q", got, body)
	}
}
