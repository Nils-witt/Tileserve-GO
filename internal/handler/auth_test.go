package handler

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// testUsername is the JWT subject used throughout this file's test tokens.
const testUsername = "alice"

// testAPIKeyUsername is the username an API-key-authenticated test request
// resolves to.
const testAPIKeyUsername = "sync-bot"

// signToken returns a signed HS256 JWT for subject, valid for ttl, using secret.
func signToken(t *testing.T, secret []byte, subject string, ttl time.Duration) string {
	t.Helper()

	claims := jwt.RegisteredClaims{
		Subject:   subject,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	return token
}

// testRSAKeyPair generates a fresh 2048-bit RSA key pair for tests.
func testRSAKeyPair(t *testing.T) *rsa.PrivateKey {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	return key
}

// signAPIKeyToken returns a signed RS256 JWT naming keyID as its `kid`
// header, with the given subject (which a correct server implementation
// must ignore in favor of the DB-resolved username) and lifetime.
func signAPIKeyToken(t *testing.T, key *rsa.PrivateKey, keyID uuid.UUID, subject string, iat time.Time, ttl time.Duration) string {
	t.Helper()

	claims := jwt.RegisteredClaims{
		Subject:   subject,
		IssuedAt:  jwt.NewNumericDate(iat),
		ExpiresAt: jwt.NewNumericDate(iat.Add(ttl)),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = keyID.String()

	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign api key token: %v", err)
	}

	return signed
}

// fakeAPIKeyResolver is a minimal apiKeySigningKeyResolver for tests that
// don't need a live Postgres connection: it resolves exactly one key id to
// one username/public key.
type fakeAPIKeyResolver struct {
	keyID        uuid.UUID
	username     string
	publicKeyPEM string
}

func (f fakeAPIKeyResolver) ResolveAPIKeySigningKey(_ context.Context, id uuid.UUID) (string, string, error) {
	if f.publicKeyPEM != "" && id == f.keyID {
		return f.username, f.publicKeyPEM, nil
	}

	return "", "", errInvalidAPIKeyForTest
}

var errInvalidAPIKeyForTest = errors.New("invalid or revoked api key")

// pemEncodePublicKey PKIX-PEM-encodes key's public half, mirroring what
// GenerateKeyPairHandler and store.CreateAPIKey deal in.
func pemEncodePublicKey(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()

	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

// parseBearerTokenCase is one table entry shared by TestParseBearerToken and
// TestParseBearerTokenAPIKey.
type parseBearerTokenCase struct {
	name         string
	authHeader   string
	queryToken   string
	wantUsername string
	wantAPIKeyID uuid.UUID
	wantHadToken bool
	wantValid    bool
}

// runParseBearerTokenCases runs tests against secret/resolver, one subtest
// per case.
func runParseBearerTokenCases(t *testing.T, secret []byte, resolver apiKeySigningKeyResolver, tests []parseBearerTokenCase) {
	t.Helper()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/maps", nil)
			if tc.authHeader != "" {
				r.Header.Set("Authorization", tc.authHeader)
			}

			if tc.queryToken != "" {
				q := r.URL.Query()
				q.Set("token", tc.queryToken)
				r.URL.RawQuery = q.Encode()
			}

			username, apiKeyID, hadToken, valid := parseBearerToken(secret, resolver, r)
			if username != tc.wantUsername || apiKeyID != tc.wantAPIKeyID || hadToken != tc.wantHadToken || valid != tc.wantValid {
				t.Fatalf("parseBearerToken() = (%q, %s, %v, %v), want (%q, %s, %v, %v)",
					username, apiKeyID, hadToken, valid, tc.wantUsername, tc.wantAPIKeyID, tc.wantHadToken, tc.wantValid)
			}
		})
	}
}

func TestParseBearerToken(t *testing.T) {
	t.Parallel()

	secret := []byte("test-secret")
	validToken := signToken(t, secret, testUsername, time.Hour)
	expiredToken := signToken(t, secret, testUsername, -time.Hour)
	wrongSecretToken := signToken(t, []byte("other-secret"), testUsername, time.Hour)
	resolver := fakeAPIKeyResolver{}

	runParseBearerTokenCases(t, secret, resolver, []parseBearerTokenCase{
		{
			name:         "no token at all",
			wantUsername: "",
			wantHadToken: false,
			wantValid:    false,
		},
		{
			name:         "valid bearer header",
			authHeader:   "Bearer " + validToken,
			wantUsername: testUsername,
			wantHadToken: true,
			wantValid:    true,
		},
		{
			name:         "valid token via query param",
			queryToken:   validToken,
			wantUsername: testUsername,
			wantHadToken: true,
			wantValid:    true,
		},
		{
			name:         "expired token",
			authHeader:   "Bearer " + expiredToken,
			wantUsername: "",
			wantHadToken: true,
			wantValid:    false,
		},
		{
			name:         "wrong signing secret",
			authHeader:   "Bearer " + wrongSecretToken,
			wantUsername: "",
			wantHadToken: true,
			wantValid:    false,
		},
		{
			name:         "malformed token",
			authHeader:   "Bearer not-a-jwt",
			wantUsername: "",
			wantHadToken: true,
			wantValid:    false,
		},
		{
			name:         "header missing Bearer prefix falls back to query, which is also empty",
			authHeader:   validToken,
			wantUsername: "",
			wantHadToken: false,
			wantValid:    false,
		},
	})
}

// TestParseBearerTokenAPIKey covers the RS256/kid dispatch path — split out
// from TestParseBearerToken to keep each test function a manageable length.
func TestParseBearerTokenAPIKey(t *testing.T) {
	t.Parallel()

	secret := []byte("test-secret")

	apiKey := testRSAKeyPair(t)
	otherKey := testRSAKeyPair(t)
	keyID := uuid.New()
	resolver := fakeAPIKeyResolver{keyID: keyID, username: testAPIKeyUsername, publicKeyPEM: pemEncodePublicKey(t, apiKey)}

	now := time.Now()
	validAPIKeyToken := signAPIKeyToken(t, apiKey, keyID, "spoofed-subject-should-be-ignored", now, 5*time.Minute)
	tooLongAPIKeyToken := signAPIKeyToken(t, apiKey, keyID, testAPIKeyUsername, now, 20*time.Minute)
	wrongKeyAPIKeyToken := signAPIKeyToken(t, otherKey, keyID, testAPIKeyUsername, now, 5*time.Minute)
	unknownKidToken := signAPIKeyToken(t, apiKey, uuid.New(), testAPIKeyUsername, now, 5*time.Minute)

	runParseBearerTokenCases(t, secret, resolver, []parseBearerTokenCase{
		{
			name:         "valid api key token resolves to the registered username, not the claimed subject",
			authHeader:   "Bearer " + validAPIKeyToken,
			wantUsername: testAPIKeyUsername,
			wantAPIKeyID: keyID,
			wantHadToken: true,
			wantValid:    true,
		},
		{
			name:         "api key token with unknown kid",
			authHeader:   "Bearer " + unknownKidToken,
			wantUsername: "",
			wantHadToken: true,
			wantValid:    false,
		},
		{
			name:         "api key token signed by the wrong private key",
			authHeader:   "Bearer " + wrongKeyAPIKeyToken,
			wantUsername: "",
			wantHadToken: true,
			wantValid:    false,
		},
		{
			name:         "api key token exceeding the max lifetime is rejected",
			authHeader:   "Bearer " + tooLongAPIKeyToken,
			wantUsername: "",
			wantHadToken: true,
			wantValid:    false,
		},
	})
}

// authProbe wraps an http.HandlerFunc with an isolated record of whether it
// was invoked, and with which context username, for one subtest.
type authProbe struct {
	called      bool
	username    string
	apiKeyID    uuid.UUID
	hasAPIKeyID bool
}

func (p *authProbe) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.called = true
		p.username = usernameFromContext(r.Context())
		p.apiKeyID, p.hasAPIKeyID = apiKeyIDFromContext(r.Context())

		w.WriteHeader(http.StatusOK)
	})
}

// assertInvalidTokenRejected checks that wrap(secret, resolver, next) rejects
// a garbage bearer token with a 401 without ever calling next. Shared by
// TestRequireAuth and TestOptionalAuth, since both middlewares reject an
// invalid token identically.
func assertInvalidTokenRejected(t *testing.T, secret []byte, resolver apiKeySigningKeyResolver, wrap func([]byte, apiKeySigningKeyResolver, http.Handler) http.Handler) {
	t.Helper()

	probe := &authProbe{}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/maps", nil)
	r.Header.Set("Authorization", "Bearer garbage")

	w := httptest.NewRecorder()
	wrap(secret, resolver, probe.handler()).ServeHTTP(w, r)

	if probe.called {
		t.Fatal("next should not be called with an invalid token")
	}

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuth(t *testing.T) {
	t.Parallel()

	secret := []byte("test-secret")
	validToken := signToken(t, secret, testUsername, time.Hour)
	resolver := fakeAPIKeyResolver{}

	t.Run("missing token is rejected", func(t *testing.T) {
		t.Parallel()

		probe := &authProbe{}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/maps", nil)
		w := httptest.NewRecorder()
		RequireAuth(secret, resolver, probe.handler()).ServeHTTP(w, r)

		if probe.called {
			t.Fatal("next should not be called without a token")
		}

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("invalid token is rejected", func(t *testing.T) {
		t.Parallel()
		assertInvalidTokenRejected(t, secret, resolver, RequireAuth)
	})

	t.Run("valid token passes through with username in context", func(t *testing.T) {
		t.Parallel()

		probe := &authProbe{}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/maps", nil)
		r.Header.Set("Authorization", "Bearer "+validToken)

		w := httptest.NewRecorder()
		RequireAuth(secret, resolver, probe.handler()).ServeHTTP(w, r)

		if !probe.called {
			t.Fatal("next should be called with a valid token")
		}

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}

		if probe.username != testUsername {
			t.Fatalf("username in context = %q, want %q", probe.username, testUsername)
		}

		if probe.hasAPIKeyID {
			t.Fatalf("apiKeyID in context = %v, want absent for a login token", probe.apiKeyID)
		}
	})

	t.Run("valid api key token passes through with the registered username and key id in context", func(t *testing.T) {
		t.Parallel()

		apiKey := testRSAKeyPair(t)
		keyID := uuid.New()
		keyResolver := fakeAPIKeyResolver{keyID: keyID, username: testAPIKeyUsername, publicKeyPEM: pemEncodePublicKey(t, apiKey)}
		token := signAPIKeyToken(t, apiKey, keyID, testAPIKeyUsername, time.Now(), 5*time.Minute)

		probe := &authProbe{}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/maps", nil)
		r.Header.Set("Authorization", "Bearer "+token)

		w := httptest.NewRecorder()
		RequireAuth(secret, keyResolver, probe.handler()).ServeHTTP(w, r)

		if !probe.called {
			t.Fatal("next should be called with a valid api key token")
		}

		if probe.username != testAPIKeyUsername {
			t.Fatalf("username in context = %q, want %q", probe.username, testAPIKeyUsername)
		}

		if !probe.hasAPIKeyID || probe.apiKeyID != keyID {
			t.Fatalf("apiKeyID in context = (%v, %v), want (%v, true)", probe.apiKeyID, probe.hasAPIKeyID, keyID)
		}
	})
}

func TestOptionalAuth(t *testing.T) {
	t.Parallel()

	secret := []byte("test-secret")
	validToken := signToken(t, secret, testUsername, time.Hour)
	resolver := fakeAPIKeyResolver{}

	t.Run("missing token passes through anonymously", func(t *testing.T) {
		t.Parallel()

		probe := &authProbe{}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/maps/some-id/version/1/0/0.png", nil)
		w := httptest.NewRecorder()
		OptionalAuth(secret, resolver, probe.handler()).ServeHTTP(w, r)

		if !probe.called {
			t.Fatal("next should be called even without a token")
		}

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}

		if probe.username != "" {
			t.Fatalf("username in context = %q, want empty", probe.username)
		}
	})

	t.Run("invalid token is still rejected", func(t *testing.T) {
		t.Parallel()
		assertInvalidTokenRejected(t, secret, resolver, OptionalAuth)
	})

	t.Run("valid token passes through with username in context", func(t *testing.T) {
		t.Parallel()

		probe := &authProbe{}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/maps", nil)
		r.Header.Set("Authorization", "Bearer "+validToken)

		w := httptest.NewRecorder()
		OptionalAuth(secret, resolver, probe.handler()).ServeHTTP(w, r)

		if !probe.called {
			t.Fatal("next should be called with a valid token")
		}

		if probe.username != testUsername {
			t.Fatalf("username in context = %q, want %q", probe.username, testUsername)
		}
	})
}
