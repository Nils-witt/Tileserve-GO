// Package handler implements the HTTP handlers for tileserve-go: JWT
// authentication, the maps/versions/geo-objects API, tile archive uploads,
// and the bundled management UI.
package handler

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

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

const (
	defaultTokenTTL = 1 * time.Hour
	maxTokenTTL     = 7 * 24 * time.Hour

	// refreshTokenTTL is how long a refresh token remains redeemable. It
	// deliberately outlives login token TTLs so a client can stay signed in
	// by refreshing well after its short-lived login token has expired.
	refreshTokenTTL = 30 * 24 * time.Hour

	// maxAPIKeyTokenLifetime is the server-enforced ceiling on an API-key
	// JWT's exp-iat gap, checked by hand after parsing regardless of what
	// the token itself claims (jwt/v5 has no ParserOption that enforces
	// this) — it's what keeps an API-key JWT short-lived even though the
	// caller, who holds the private key, controls exp/iat.
	maxAPIKeyTokenLifetime = 15 * time.Minute
)

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

// issueLoginToken signs and returns a human login JWT for username, valid
// for ttl. Shared by LoginHandler, RefreshHandler, and the OIDC callback
// (see oidc.go) — every path that hands a caller a fresh session ends here.
func issueLoginToken(secret []byte, username string, ttl time.Duration) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   username,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}

	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
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

		username, err := authenticatePassword(r.Context(), st, ldapAuth, req.Username, req.Password)
		if err != nil {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		ttl := defaultTokenTTL
		if req.TTLSeconds > 0 {
			ttl = min(time.Duration(req.TTLSeconds)*time.Second, maxTokenTTL)
		}

		token, err := issueLoginToken(secret, username, ttl)
		if err != nil {
			http.Error(w, "failed to issue token", http.StatusInternalServerError)
			return
		}

		refreshToken, _, err := st.CreateRefreshToken(r.Context(), username, refreshTokenTTL)
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
		if !requireMethod(w, r, http.MethodPost) {
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

		username, newRefreshToken, _, err := st.RotateRefreshToken(r.Context(), req.RefreshToken, refreshTokenTTL)
		if err != nil {
			writeStoreError(w, err, store.ErrInvalidRefreshToken, http.StatusUnauthorized, "invalid or expired refresh token", "failed to refresh token")
			return
		}

		token, err := issueLoginToken(secret, username, defaultTokenTTL)
		if err != nil {
			http.Error(w, "failed to issue token", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		//nolint:gosec // this endpoint's purpose is to hand the refresh token to the client
		_ = json.NewEncoder(w).Encode(loginResponse{Token: token, RefreshToken: newRefreshToken})
	}
}

// apiKeySigningKeyResolver resolves a registered API key's id — which IS
// the JWT `kid` header a caller sets — to the username it authenticates as
// and its registered RSA public key PEM. *store.Store satisfies this
// automatically via ResolveAPIKeySigningKey; it's declared as its own
// interface here (rather than depending on *store.Store directly) so tests
// can exercise the kid-less HS256 login path of parseBearerToken/
// authMiddleware by passing nil — a token with no `kid` header never reaches
// the resolver, so nil is never dereferenced.
type apiKeySigningKeyResolver interface {
	ResolveAPIKeySigningKey(ctx context.Context, id uuid.UUID) (username, publicKeyPEM string, err error)
}

// resolveTokenKey returns the jwt.Keyfunc parseBearerToken parses with: a
// token with no `kid` header is a human login/refresh JWT, verified against
// secret; one WITH a `kid` header is an API-key JWT, verified against the
// public key registered for that key id via resolver. A keyfunc has no way
// to report anything beyond the signing key itself, so on a successful
// API-key resolution it records the DB-resolved identity into
// *resolvedUsername/*isAPIKeyToken/*resolvedAPIKeyID for parseBearerToken to
// use afterward — that identity, not the token's own `sub` claim, is what a
// caller ultimately authenticates as (see parseBearerToken's doc comment).
func resolveTokenKey(ctx context.Context, secret []byte, resolver apiKeySigningKeyResolver, resolvedUsername *string, isAPIKeyToken *bool, resolvedAPIKeyID *uuid.UUID) jwt.Keyfunc {
	return func(t *jwt.Token) (any, error) {
		kidRaw, hasKid := t.Header["kid"]
		if !hasKid {
			if t.Method != jwt.SigningMethodHS256 {
				return nil, jwt.ErrTokenSignatureInvalid
			}

			return secret, nil
		}

		if t.Method != jwt.SigningMethodRS256 {
			return nil, jwt.ErrTokenSignatureInvalid
		}

		kidStr, ok := kidRaw.(string)
		if !ok {
			return nil, jwt.ErrTokenMalformed
		}

		keyID, err := uuid.Parse(kidStr)
		if err != nil {
			return nil, jwt.ErrTokenMalformed
		}

		uname, publicKeyPEM, err := resolver.ResolveAPIKeySigningKey(ctx, keyID)
		if err != nil {
			return nil, err
		}

		publicKey, err := jwt.ParseRSAPublicKeyFromPEM([]byte(publicKeyPEM))
		if err != nil {
			return nil, err
		}

		*isAPIKeyToken = true
		*resolvedUsername = uname
		*resolvedAPIKeyID = keyID

		return publicKey, nil
	}
}

// apiKeyTokenWithinLifetime reports whether claims' exp-iat gap is within
// maxAPIKeyTokenLifetime. jwt/v5 has no ParserOption that enforces this (its
// WithIssuedAt only rejects a future iat when one is present, it doesn't
// require one), so it's checked by hand — the caller, who holds the private
// key, otherwise fully controls exp/iat.
func apiKeyTokenWithinLifetime(claims *jwt.RegisteredClaims) bool {
	if claims.IssuedAt == nil || claims.ExpiresAt == nil {
		return false
	}

	return claims.ExpiresAt.Sub(claims.IssuedAt.Time) <= maxAPIKeyTokenLifetime
}

// parseBearerToken extracts and validates a bearer credential from the
// request's Authorization header or ?token= query parameter. Every
// credential is a JWT: one with no `kid` header is a human login/refresh
// token (HS256, shared secret, subject trusted as claimed); one WITH a
// `kid` header is an API-key token (RS256, verified against the public key
// registered for that key id via resolver) whose identity comes from that
// DB lookup, never from the token's own `sub` claim — a caller can't claim
// to be a different user than the one their key is registered under just by
// setting a different subject. API-key tokens are additionally capped at
// maxAPIKeyTokenLifetime regardless of what they claim (see
// apiKeyTokenWithinLifetime). hadToken is false if the request supplied no
// token at all (distinct from supplying an invalid one), so callers can
// tell "anonymous" apart from "bad credentials". apiKeyID is the zero UUID
// for a human login/refresh token (which never carries one); it is only
// ever non-zero alongside a successfully validated API-key token.
func parseBearerToken(secret []byte, resolver apiKeySigningKeyResolver, r *http.Request) (username string, apiKeyID uuid.UUID, hadToken, valid bool) {
	tokenString, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || tokenString == "" {
		tokenString = r.URL.Query().Get("token")
	}

	if tokenString == "" {
		return "", uuid.Nil, false, false
	}

	var (
		resolvedUsername string
		isAPIKeyToken    bool
		resolvedAPIKeyID uuid.UUID
	)

	claims := &jwt.RegisteredClaims{}
	keyfunc := resolveTokenKey(r.Context(), secret, resolver, &resolvedUsername, &isAPIKeyToken, &resolvedAPIKeyID)

	token, err := jwt.ParseWithClaims(tokenString, claims, keyfunc, jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return "", uuid.Nil, true, false
	}

	if !isAPIKeyToken {
		return claims.Subject, uuid.Nil, true, true
	}

	if !apiKeyTokenWithinLifetime(claims) {
		return "", uuid.Nil, true, false
	}

	return resolvedUsername, resolvedAPIKeyID, true, true
}

// authMiddleware is the shared core of RequireAuth and OptionalAuth: it
// parses the bearer token and stores its subject (username, possibly empty)
// in the request context for next to read via usernameFromContext. A token
// that IS present but invalid/expired is always rejected with 401; whether a
// missing token is also rejected depends on requireToken.
func authMiddleware(secret []byte, resolver apiKeySigningKeyResolver, next http.Handler, requireToken bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, apiKeyID, hadToken, valid := parseBearerToken(secret, resolver, r)
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

// RequireAuth is HTTP middleware that rejects any request without a valid
// bearer token (401) and otherwise stores the token's subject (username) in
// the request context for next to read via usernameFromContext. The bearer
// token may be a login JWT or an API key JWT (see parseBearerToken).
func RequireAuth(secret []byte, resolver apiKeySigningKeyResolver, next http.Handler) http.Handler {
	return authMiddleware(secret, resolver, next, true)
}

// OptionalAuth is like RequireAuth but lets a request with no bearer token
// at all through, with an empty username in the context (see
// usernameFromContext) rather than rejecting it outright. It's for routes
// that decide per-resource whether anonymous access is allowed (e.g. a
// map's anonymousAllowed setting) — everything else on such a route must
// still call requireAuthenticated itself. A token that IS present but
// invalid/expired is still rejected with 401, same as RequireAuth.
func OptionalAuth(secret []byte, resolver apiKeySigningKeyResolver, next http.Handler) http.Handler {
	return authMiddleware(secret, resolver, next, false)
}
