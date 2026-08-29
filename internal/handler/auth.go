// Package handler implements the HTTP handlers for tileserve-go: JWT
// authentication, the maps/versions/geo-objects API, tile archive uploads,
// and the bundled management UI.
package handler

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"nilswitt.dev/tileserve-go/internal/handler/auth/jwt"
	"nilswitt.dev/tileserve-go/internal/handler/auth/ldap"
	"nilswitt.dev/tileserve-go/internal/handler/utils"

	"nilswitt.dev/tileserve-go/internal/ldapauth"
	"nilswitt.dev/tileserve-go/internal/store"
)

type contextKey string

const (
	usernameContextKey contextKey = "username"
	apiKeyIDContextKey contextKey = "apiKeyID"
)

// usernameFromContext returns the JWT subject stored by RequireAuth, or "" if absent.
func usernameFromContext(ctx context.Context) string {
	username, _ := ctx.Value(usernameContextKey).(string)
	return username
}

// apiKeyIDFromContext returns the id of the API key that authenticated this
// request, if any. It's absent for a human login/refresh token session (see
// authMiddleware) — scoping (internal/store's api_key_scopes) only ever
// restricts an API key's own access, never a human session's.
func apiKeyIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(apiKeyIDContextKey).(uuid.UUID)
	return id, ok
}

//go:embed login.html
var loginPage []byte

//go:embed login.js
var loginScript []byte

// LoginScriptHandler serves the login page's script at /login.js.
func LoginScriptHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(loginScript)
	}
}

type loginRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	TTLSeconds int64  `json:"ttl_seconds,omitempty"`
}

type loginResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

// LoginHandler serves GET /login (the static login page) and handles
// POST /login: it authenticates the given username/password — against st,
// falling back to ldapAuth (if configured) for an account that isn't a local
// match, see authenticatePassword — and, on success, issues a signed JWT
// valid for the requested TTL (capped at maxTokenTTL, defaulting to
// defaultTokenTTL) alongside a refresh token (valid for refreshTokenTTL)
// that can later be exchanged at POST /refresh for a new login JWT without
// re-sending credentials. ldapAuth may be nil, meaning LDAP login isn't
// configured.
func LoginHandler(secret []byte, st *store.Store, ldapAuth *ldapauth.Authenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(loginPage)

			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req loginRequest
		if !decodeJSON(w, r, &req) {
			return
		}

		username, err := ldap.AuthenticatePassword(r.Context(), st, ldapAuth, req.Username, req.Password)
		if err != nil {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		token, err := jwt.IssueLoginToken(secret, username)
		if err != nil {
			http.Error(w, "failed to issue token", http.StatusInternalServerError)
			return
		}

		refreshToken, _, err := st.CreateRefreshToken(r.Context(), username, jwt.RefreshTokenTTL)
		if err != nil {
			http.Error(w, "failed to issue token", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		//nolint:gosec // this endpoint's purpose is to hand the refresh token to the client
		_ = json.NewEncoder(w).Encode(loginResponse{Token: token, RefreshToken: refreshToken})
	}
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// RefreshHandler serves POST /refresh: redeems a valid, unexpired refresh
// token (as previously issued by LoginHandler or a prior call to this
// handler) for a new login JWT, plus a new refresh token that replaces it.
// The old refresh token is revoked as part of the same exchange, so each
// one is single-use; reusing an already-redeemed refresh token is treated
// as invalid, same as an unknown or expired one.
func RefreshHandler(secret []byte, st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !utils.RequireMethod(w, r, http.MethodPost) {
			return
		}

		var req refreshRequest
		if !decodeJSON(w, r, &req) {
			return
		}

		if req.RefreshToken == "" {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		username, newRefreshToken, _, err := st.RotateRefreshToken(r.Context(), req.RefreshToken, jwt.RefreshTokenTTL)
		if err != nil {
			writeStoreError(w, err, store.ErrInvalidRefreshToken, http.StatusUnauthorized, "invalid or expired refresh token", "failed to refresh token")
			return
		}

		token, err := jwt.IssueLoginToken(secret, username)
		if err != nil {
			http.Error(w, "failed to issue token", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		//nolint:gosec // this endpoint's purpose is to hand the refresh token to the client
		_ = json.NewEncoder(w).Encode(loginResponse{Token: token, RefreshToken: newRefreshToken})
	}
}

// AuthMiddleware authenticates a request's bearer token (see
// jwt.ParseBearerToken) and stores the resolved username/API-key id in
// context for next, retrievable via usernameFromContext/apiKeyIDFromContext.
// If requireToken is true, a missing token is rejected with 401 (this is
// what used to be called RequireAuth); if false, a missing token passes
// through anonymously (what used to be OptionalAuth) — either way, a
// present but invalid/expired token is always rejected.
func AuthMiddleware(secret []byte, resolver jwt.APIKeySigningKeyResolver, next http.Handler, requireToken bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, apiKeyID, hadToken, valid := jwt.ParseBearerToken(secret, resolver, r)
		if requireToken && !hadToken {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}

		if hadToken && !valid {
			http.Error(w, "invalid or expired token", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), usernameContextKey, username)
		if apiKeyID != uuid.Nil {
			ctx = context.WithValue(ctx, apiKeyIDContextKey, apiKeyID)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
