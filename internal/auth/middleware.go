package auth

import (
	"net/http"

	"github.com/google/uuid"
	"nilswitt.dev/tileserve-go/internal/auth/jwt"
)

// Middleware authenticates a request's bearer token (see
// jwt.ParseBearerToken) and stores the resolved username/API-key id in
// context for next, retrievable via UsernameFromContext/APIKeyIDFromContext.
// If requireToken is true, a missing token is rejected with 401 (this is
// what used to be called RequireAuth); if false, a missing token passes
// through anonymously (what used to be OptionalAuth) — either way, a
// present but invalid/expired token is always rejected.
func Middleware(secret []byte, resolver jwt.APIKeySigningKeyResolver, next http.Handler, requireToken bool) http.Handler {
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

		ctx := ContextWithUsername(r.Context(), username)
		if apiKeyID != uuid.Nil {
			ctx = ContextWithAPIKeyID(ctx, apiKeyID)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
