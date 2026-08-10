package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// testUsername is the JWT subject used throughout this file's test tokens.
const testUsername = "alice"

// signToken returns a signed JWT for subject, valid for ttl, using secret.
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

func TestParseBearerToken(t *testing.T) {
	t.Parallel()

	secret := []byte("test-secret")
	validToken := signToken(t, secret, testUsername, time.Hour)
	expiredToken := signToken(t, secret, testUsername, -time.Hour)
	wrongSecretToken := signToken(t, []byte("other-secret"), testUsername, time.Hour)

	tests := []struct {
		name         string
		authHeader   string
		queryToken   string
		wantUsername string
		wantHadToken bool
		wantValid    bool
	}{
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
	}

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

			username, hadToken, valid := parseBearerToken(secret, r)
			if username != tc.wantUsername || hadToken != tc.wantHadToken || valid != tc.wantValid {
				t.Fatalf("parseBearerToken() = (%q, %v, %v), want (%q, %v, %v)",
					username, hadToken, valid, tc.wantUsername, tc.wantHadToken, tc.wantValid)
			}
		})
	}
}

// authProbe wraps an http.HandlerFunc with an isolated record of whether it
// was invoked, and with which context username, for one subtest.
type authProbe struct {
	called   bool
	username string
}

func (p *authProbe) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.called = true
		p.username = usernameFromContext(r.Context())

		w.WriteHeader(http.StatusOK)
	})
}

// assertInvalidTokenRejected checks that wrap(secret, next) rejects a
// garbage bearer token with a 401 without ever calling next. Shared by
// TestRequireAuth and TestOptionalAuth, since both middlewares reject an
// invalid token identically.
func assertInvalidTokenRejected(t *testing.T, secret []byte, wrap func([]byte, http.Handler) http.Handler) {
	t.Helper()

	probe := &authProbe{}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/maps", nil)
	r.Header.Set("Authorization", "Bearer garbage")

	w := httptest.NewRecorder()
	wrap(secret, probe.handler()).ServeHTTP(w, r)

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

	t.Run("missing token is rejected", func(t *testing.T) {
		t.Parallel()

		probe := &authProbe{}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/maps", nil)
		w := httptest.NewRecorder()
		RequireAuth(secret, probe.handler()).ServeHTTP(w, r)

		if probe.called {
			t.Fatal("next should not be called without a token")
		}

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("invalid token is rejected", func(t *testing.T) {
		t.Parallel()
		assertInvalidTokenRejected(t, secret, RequireAuth)
	})

	t.Run("valid token passes through with username in context", func(t *testing.T) {
		t.Parallel()

		probe := &authProbe{}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/maps", nil)
		r.Header.Set("Authorization", "Bearer "+validToken)

		w := httptest.NewRecorder()
		RequireAuth(secret, probe.handler()).ServeHTTP(w, r)

		if !probe.called {
			t.Fatal("next should be called with a valid token")
		}

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}

		if probe.username != testUsername {
			t.Fatalf("username in context = %q, want %q", probe.username, testUsername)
		}
	})
}

func TestOptionalAuth(t *testing.T) {
	t.Parallel()

	secret := []byte("test-secret")
	validToken := signToken(t, secret, testUsername, time.Hour)

	t.Run("missing token passes through anonymously", func(t *testing.T) {
		t.Parallel()

		probe := &authProbe{}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/maps/some-id/version/1/0/0.png", nil)
		w := httptest.NewRecorder()
		OptionalAuth(secret, probe.handler()).ServeHTTP(w, r)

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
		assertInvalidTokenRejected(t, secret, OptionalAuth)
	})

	t.Run("valid token passes through with username in context", func(t *testing.T) {
		t.Parallel()

		probe := &authProbe{}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/maps", nil)
		r.Header.Set("Authorization", "Bearer "+validToken)

		w := httptest.NewRecorder()
		OptionalAuth(secret, probe.handler()).ServeHTTP(w, r)

		if !probe.called {
			t.Fatal("next should be called with a valid token")
		}

		if probe.username != testUsername {
			t.Fatalf("username in context = %q, want %q", probe.username, testUsername)
		}
	})
}
