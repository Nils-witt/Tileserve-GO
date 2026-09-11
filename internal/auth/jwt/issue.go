package jwt

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	defaultTokenTTL = 1 * time.Hour
	maxTokenTTL     = 7 * 24 * time.Hour

	// RefreshTokenTTL is how long a refresh token remains redeemable. It
	// deliberately outlives login token TTLs so a client can stay signed in
	// by refreshing well after its short-lived login token has expired.
	RefreshTokenTTL = 30 * 24 * time.Hour

	// maxAPIKeyTokenLifetime is the server-enforced ceiling on an API-key
	// JWT's exp-iat gap, checked by hand after parsing regardless of what
	// the token itself claims (jwt/v5 has no ParserOption that enforces
	// this) — it's what keeps an API-key JWT short-lived even though the
	// caller, who holds the private key, controls exp/iat.
	maxAPIKeyTokenLifetime = 15 * time.Minute
)

// IssueLoginToken signs and returns a human login JWT for username, valid
// for ttl. Shared by LoginHandler, RefreshHandler, and the OIDC callback
// (see oidc.go) — every path that hands a caller a fresh session ends here.
func IssueLoginToken(secret []byte, username string) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   username,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(defaultTokenTTL)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}

	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}
