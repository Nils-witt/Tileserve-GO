// Package auth handles authentication: JWT issuance/verification, the
// login/refresh HTTP handlers and bearer-token middleware, LDAP and OIDC
// login (see the ldap and oidc subpackages), and the request-context
// helpers used to carry the authenticated username/API-key id downstream.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"nilswitt.dev/tileserve-go/internal/auth/jwt"
	"nilswitt.dev/tileserve-go/internal/auth/ldap"
	"nilswitt.dev/tileserve-go/internal/httputil"
	"nilswitt.dev/tileserve-go/internal/store"
)

type loginRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	TTLSeconds int64  `json:"ttl_seconds,omitempty"`
}

type loginResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

// LoginHandler serves POST /login: it authenticates the given
// username/password — against st, falling back to ldapAuth (if configured)
// for an account that isn't a local match, see authenticatePassword — and,
// on success, issues a signed JWT valid for the requested TTL (capped at
// maxTokenTTL, defaulting to defaultTokenTTL) alongside a refresh token
// (valid for refreshTokenTTL) that can later be exchanged at POST /refresh
// for a new login JWT without re-sending credentials. ldapAuth may be nil,
// meaning LDAP login isn't configured. The login page itself is served by
// the frontend SPA (see internal/webserver/spa) at GET /login.
func LoginHandler(secret []byte, st *store.Store, ldapAuth *ldap.Authenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !httputil.RequireMethod(w, r, http.MethodPost) {
			return
		}

		var req loginRequest
		if !httputil.DecodeJSON(w, r, &req) {
			return
		}

		username, err := validateCredentials(r.Context(), st, ldapAuth, req.Username, req.Password)
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

func validateCredentials(ctx context.Context, st *store.Store, ldapAuth *ldap.Authenticator, username, password string) (string, error) {
	if username == "" || password == "" {
		return "", errors.New("username and password must not be empty")
	}

	err := st.Authenticate(ctx, username, password)
	if err == nil {
		return username, nil
	}

	if !errors.Is(err, store.ErrInvalidCredentials) {
		return "", err
	}

	username, ldapErr := ldap.AuthenticatePassword(ctx, st, ldapAuth, username, password)
	if ldapErr != nil {
		return "", ldapErr
	}

	return username, nil
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// writeStoreError maps a Store error to an HTTP response: sentinel maps to
// sentinelStatus with sentinelMsg (e.g. a not-found or validation error),
// any other non-nil error maps to a 500 with failMsg.
func writeStoreError(w http.ResponseWriter, err, sentinel error, sentinelStatus int, sentinelMsg, failMsg string) {
	if errors.Is(err, sentinel) {
		http.Error(w, sentinelMsg, sentinelStatus)
		return
	}

	http.Error(w, failMsg, http.StatusInternalServerError)
}

// RefreshHandler serves POST /refresh: redeems a valid, unexpired refresh
// token (as previously issued by LoginHandler or a prior call to this
// handler) for a new login JWT, plus a new refresh token that replaces it.
// The old refresh token is revoked as part of the same exchange, so each
// one is single-use; reusing an already-redeemed refresh token is treated
// as invalid, same as an unknown or expired one.
func RefreshHandler(secret []byte, st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !httputil.RequireMethod(w, r, http.MethodPost) {
			return
		}

		var req refreshRequest
		if !httputil.DecodeJSON(w, r, &req) {
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
