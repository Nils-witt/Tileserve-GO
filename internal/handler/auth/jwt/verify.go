package jwt

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// TokenWithinLifetime reports whether claims' exp-iat gap is within
// maxAPIKeyTokenLifetime, the server-enforced ceiling on an API-key JWT's
// lifetime regardless of what the token itself claims.
func TokenWithinLifetime(claims *jwt.RegisteredClaims) bool {
	if claims.IssuedAt == nil || claims.ExpiresAt == nil {
		return false
	}

	return claims.ExpiresAt.Sub(claims.IssuedAt.Time) <= maxAPIKeyTokenLifetime
}

// APIKeySigningKeyResolver resolves a registered API key's id — which IS
// the JWT `kid` header a caller sets — to the username it authenticates as
// and its registered RSA public key PEM. *store.Store satisfies this
// automatically via ResolveAPIKeySigningKey; it's declared as its own
// interface here (rather than depending on *store.Store directly) so tests
// can exercise the kid-less HS256 login path of parseBearerToken/
// authMiddleware by passing nil — a token with no `kid` header never reaches
// the resolver, so nil is never dereferenced.
type APIKeySigningKeyResolver interface {
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
func resolveTokenKey(ctx context.Context, secret []byte, resolver APIKeySigningKeyResolver, resolvedUsername *string, isAPIKeyToken *bool, resolvedAPIKeyID *uuid.UUID) jwt.Keyfunc {
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

// ParseBearerToken extracts and validates a bearer credential from the
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
func ParseBearerToken(secret []byte, resolver APIKeySigningKeyResolver, r *http.Request) (username string, apiKeyID uuid.UUID, hadToken, valid bool) {
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

	if !TokenWithinLifetime(claims) {
		return "", uuid.Nil, true, false
	}

	return resolvedUsername, resolvedAPIKeyID, true, true
}
