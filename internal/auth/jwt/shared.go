// Package jwt issues and verifies the JWTs tileserve-go uses for both human
// login/refresh sessions (HS256, shared secret) and API-key authentication
// (RS256, per-key registered public key) — see APIKeySigningKeyResolver and
// ParseBearerToken.
package jwt
