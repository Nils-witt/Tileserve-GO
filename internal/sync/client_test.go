package sync

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"nilswitt.dev/tileserve-go/internal/store"
)

// testKeyPair generates a fresh RSA key pair for tests, matching what
// NewClient signs with.
func testKeyPair(t *testing.T) *rsa.PrivateKey {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	return key
}

// requireAPIKeyJWT wraps handler, rejecting any request whose bearer token
// isn't a validly-signed RS256 JWT naming wantKeyID as its `kid` and
// verifiable against wantKey's public half — mimicking the remote server's
// own auth gate closely enough to exercise Client's request-building.
func requireAPIKeyJWT(t *testing.T, wantKeyID uuid.UUID, wantKey *rsa.PrivateKey, handler http.HandlerFunc) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		tokenString, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		claims := &jwt.RegisteredClaims{}

		token, err := jwt.ParseWithClaims(tokenString, claims, func(tok *jwt.Token) (any, error) {
			kid, _ := tok.Header["kid"].(string)
			if kid != wantKeyID.String() {
				return nil, jwt.ErrTokenSignatureInvalid
			}

			if tok.Method != jwt.SigningMethodRS256 {
				return nil, jwt.ErrTokenSignatureInvalid
			}

			return &wantKey.PublicKey, nil
		}, jwt.WithExpirationRequired())
		if err != nil || !token.Valid {
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

	key := testKeyPair(t)
	keyID := uuid.New()

	mux := http.NewServeMux()
	mux.HandleFunc("/maps", requireAPIKeyJWT(t, keyID, key, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", r.Method)
		}

		_ = json.NewEncoder(w).Encode(want)
	}))

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewClient(srv.URL, keyID, key)

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

	key := testKeyPair(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/maps", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewClient(srv.URL, uuid.New(), key)

	if _, err := client.ListMaps(t.Context()); err == nil {
		t.Fatal("ListMaps() with a rejected request should return an error")
	}
}

func TestClientDownloadArchive(t *testing.T) {
	t.Parallel()

	mapID := uuid.New()
	body := []byte("fake-zip-bytes")

	key := testKeyPair(t)
	keyID := uuid.New()

	mux := http.NewServeMux()
	mux.HandleFunc("/maps/"+mapID.String()+"/version/3/archive", requireAPIKeyJWT(t, keyID, key, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewClient(srv.URL, keyID, key)

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
