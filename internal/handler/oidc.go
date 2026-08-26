package handler

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"nilswitt.dev/tileserve-go/internal/store"
)

// OIDCAuthenticator holds the state needed to run the OpenID Connect
// authorization code flow against one configured provider: discovery
// document, ID token verifier, and OAuth2 client config. Construct one with
// NewOIDCAuthenticator at startup; a nil *OIDCAuthenticator means OIDC login
// isn't configured, and /login/oidc[/callback] aren't registered at all (see
// cmd/tileserve-go/main.go).
type OIDCAuthenticator struct {
	verifier     *oidc.IDTokenVerifier
	oauth2Config oauth2.Config
}

// NewOIDCAuthenticator discovers the OIDC provider at issuerURL (via its
// /.well-known/openid-configuration document) and builds an authenticator
// for the given client, redirecting back to redirectURL after login.
func NewOIDCAuthenticator(ctx context.Context, issuerURL, clientID, clientSecret, redirectURL string) (*OIDCAuthenticator, error) {
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover oidc provider %q: %w", issuerURL, err)
	}

	return &OIDCAuthenticator{
		verifier: provider.Verifier(&oidc.Config{ClientID: clientID}),
		oauth2Config: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
	}, nil
}

// AuthMethodsHandler serves GET /auth/methods (public): reports which login
// methods this server currently exposes, so the login pages' script can
// show or hide the "Sign in with SSO" button without hardcoding whether OIDC
// is configured.
func AuthMethodsHandler(oidcEnabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}

		writeJSON(w, http.StatusOK, map[string]bool{"oidc": oidcEnabled})
	}
}

const (
	oidcStateCookie    = "tileserve_oidc_state"
	oidcNonceCookie    = "tileserve_oidc_nonce"
	oidcRedirectCookie = "tileserve_oidc_redirect" //nolint:gosec // G101 false positive: a cookie name constant, not a credential
	// oidcCookiePath scopes the state/nonce/redirect cookies to the OIDC
	// routes alone (a browser sends a cookie with this Path for any request
	// path that path or one nested under it, so it covers both
	// /login/oidc and /login/oidc/callback below).
	oidcCookiePath = "/login/oidc"
	// oidcFlowTTL bounds how long a caller has to complete the redirect to
	// the provider and back before its state/nonce cookies expire.
	oidcFlowTTL = 5 * time.Minute
)

// OIDCLoginHandler serves GET /login/oidc: it starts the authorization code
// flow by generating a fresh CSRF state value and replay-resistant nonce,
// stashing both (plus the post-login redirect target) in short-lived
// cookies, and redirecting the browser to the provider's authorization
// endpoint. OIDCCallbackHandler verifies the state and nonce against these
// same cookies when the provider redirects back.
func OIDCLoginHandler(auth *OIDCAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}

		state, err := randomOIDCValue()
		if err != nil {
			http.Error(w, "failed to start sign-in", http.StatusInternalServerError)
			return
		}

		nonce, err := randomOIDCValue()
		if err != nil {
			http.Error(w, "failed to start sign-in", http.StatusInternalServerError)
			return
		}

		redirectTo := sanitizeRedirectPath(r.URL.Query().Get("redirect"))

		setOIDCCookie(w, r, oidcStateCookie, state)
		setOIDCCookie(w, r, oidcNonceCookie, nonce)
		setOIDCCookie(w, r, oidcRedirectCookie, redirectTo)

		http.Redirect(w, r, auth.oauth2Config.AuthCodeURL(state, oidc.Nonce(nonce)), http.StatusFound)
	}
}

// OIDCCallbackHandler serves GET /login/oidc/callback: it validates the
// state/nonce round-tripped via cookies (see OIDCLoginHandler), exchanges
// the authorization code for an ID token, resolves the token's issuer+
// subject to a local account — auto-provisioning one on first login — and
// issues that account a normal login JWT and refresh token exactly like
// LoginHandler, handing them to the browser via the redirect target's URL
// fragment (never a query parameter, so they don't end up in server logs or
// Referer headers).
func OIDCCallbackHandler(auth *OIDCAuthenticator, secret []byte, st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}

		redirectTo, expectedState, expectedNonce := consumeOIDCFlowCookies(w, r)

		if errParam := r.URL.Query().Get("error"); errParam != "" {
			http.Error(w, "sign-in failed: "+errParam, http.StatusUnauthorized)
			return
		}

		state := r.URL.Query().Get("state")
		if expectedState == "" || subtle.ConstantTimeCompare([]byte(state), []byte(expectedState)) != 1 {
			http.Error(w, "invalid sign-in state", http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing authorization code", http.StatusBadRequest)
			return
		}

		ctx := r.Context()

		idToken, claims, err := auth.exchangeAndVerify(ctx, code, expectedNonce)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		username, err := resolveOIDCUsername(ctx, st, idToken, claims)
		if err != nil {
			http.Error(w, "failed to resolve account", http.StatusInternalServerError)
			return
		}

		token, refreshToken, err := issueOIDCSession(ctx, st, secret, username)
		if err != nil {
			http.Error(w, "failed to issue token", http.StatusInternalServerError)
			return
		}

		fragment := url.Values{}
		fragment.Set("token", token)
		fragment.Set("refresh_token", refreshToken)
		fragment.Set("username", username)

		http.Redirect(w, r, redirectTo+"#"+fragment.Encode(), http.StatusFound)
	}
}

// consumeOIDCFlowCookies reads and clears the state/nonce/redirect cookies
// set by OIDCLoginHandler, returning a safe redirect target (see
// sanitizeRedirectPath) alongside the expected state and nonce — either may
// be "" if its cookie was missing or had already expired.
func consumeOIDCFlowCookies(w http.ResponseWriter, r *http.Request) (redirectTo, state, nonce string) {
	state, _ = readAndClearOIDCCookie(w, r, oidcStateCookie)
	nonce, _ = readAndClearOIDCCookie(w, r, oidcNonceCookie)
	redirectTo, _ = readAndClearOIDCCookie(w, r, oidcRedirectCookie)

	return sanitizeRedirectPath(redirectTo), state, nonce
}

// oidcClaims is the subset of ID token profile claims used to resolve or
// provision a local account.
type oidcClaims struct {
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
}

// exchangeAndVerify exchanges an authorization code for tokens, verifies the
// returned ID token — including that its nonce matches expectedNonce, which
// rules out a replayed token from a different sign-in attempt — and decodes
// its profile claims. Every error is already a safe, generic message: it's
// returned as-is for the caller to hand straight to http.Error.
func (auth *OIDCAuthenticator) exchangeAndVerify(ctx context.Context, code, expectedNonce string) (*oidc.IDToken, oidcClaims, error) {
	oauth2Token, err := auth.oauth2Config.Exchange(ctx, code)
	if err != nil {
		return nil, oidcClaims{}, errors.New("failed to exchange authorization code")
	}

	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, oidcClaims{}, errors.New("provider response did not include an id token")
	}

	idToken, err := auth.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, oidcClaims{}, errors.New("failed to verify id token")
	}

	if expectedNonce == "" || subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(expectedNonce)) != 1 {
		return nil, oidcClaims{}, errors.New("invalid sign-in nonce")
	}

	var claims oidcClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, oidcClaims{}, errors.New("failed to read id token claims")
	}

	return idToken, claims, nil
}

// resolveOIDCUsername resolves idToken's issuer+subject to a local account,
// auto-provisioning one via store.CreateOIDCUser on first login at that
// identity.
func resolveOIDCUsername(ctx context.Context, st *store.Store, idToken *oidc.IDToken, claims oidcClaims) (string, error) {
	username, err := st.FindUserByOIDCIdentity(ctx, idToken.Issuer, idToken.Subject)
	if err == nil {
		return username, nil
	}

	if !errors.Is(err, store.ErrUserNotFound) {
		return "", err
	}

	candidate := firstNonEmpty(claims.PreferredUsername, claims.Email, idToken.Subject)

	u, err := st.CreateOIDCUser(ctx, candidate, idToken.Issuer, idToken.Subject)
	if err != nil {
		return "", err
	}

	return u.Username, nil
}

// issueOIDCSession issues a login JWT and refresh token for username, same
// as a password login (see issueLoginToken/LoginHandler).
func issueOIDCSession(ctx context.Context, st *store.Store, secret []byte, username string) (token, refreshToken string, err error) {
	token, err = issueLoginToken(secret, username, defaultTokenTTL)
	if err != nil {
		return "", "", err
	}

	refreshToken, _, err = st.CreateRefreshToken(ctx, username, refreshTokenTTL)
	if err != nil {
		return "", "", err
	}

	return token, refreshToken, nil
}

// firstNonEmpty returns the first non-empty string among values, or "" if
// all are empty.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}

// sanitizeRedirectPath returns path if it's a safe same-origin redirect
// target (an absolute path, no scheme or host smuggled in), otherwise the
// UI's own default. This is what stands between /login/oidc?redirect=... and
// an open redirect that walks off with the freshly issued token handed to
// the callback's own redirect target via a URL fragment.
func sanitizeRedirectPath(path string) string {
	if path == "" || path[0] != '/' || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\\") {
		return "/ui/"
	}

	return path
}

// randomOIDCValue returns a random, URL-safe string suitable for an OAuth2
// state or nonce value.
func randomOIDCValue() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate oidc state/nonce: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// requestIsHTTPS reports whether r arrived over HTTPS, directly or (per
// convention) via a reverse proxy that terminated TLS and set
// X-Forwarded-Proto — used to decide whether the OIDC flow cookies get the
// Secure attribute.
func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// setOIDCCookie sets one of the flow cookies (state/nonce/redirect),
// scoped to oidcCookiePath and expiring after oidcFlowTTL.
func setOIDCCookie(w http.ResponseWriter, r *http.Request, name, value string) {
	//nolint:gosec // G124 false positive: Secure is set via requestIsHTTPS(r) below; gosec's checker only recognizes a literal bool
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     oidcCookiePath,
		MaxAge:   int(oidcFlowTTL.Seconds()),
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// readAndClearOIDCCookie reads one of the flow cookies and immediately
// expires it (single-use, whether or not the caller ends up using the
// value) — a stale state/nonce/redirect cookie from an abandoned sign-in
// attempt must never be reusable by a later one.
func readAndClearOIDCCookie(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	//nolint:gosec // G124 false positive: Secure is set via requestIsHTTPS(r) below; gosec's checker only recognizes a literal bool
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     oidcCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})

	c, err := r.Cookie(name)
	if err != nil {
		return "", false
	}

	return c.Value, true
}
