package store

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// minRSAKeyBits is the smallest RSA modulus size accepted for an API key's
// public key.
const minRSAKeyBits = 2048

var (
	// ErrInvalidAPIKey is returned when an API key JWT names an unknown or revoked key.
	ErrInvalidAPIKey = errors.New("invalid or revoked api key")
	// ErrAPIKeyNotFound is returned when looking up a specific key by id finds no row.
	ErrAPIKeyNotFound = errors.New("api key not found")
	// ErrInvalidPublicKeyPEM is returned when a submitted public key isn't a
	// PEM-encoded RSA public key of at least minRSAKeyBits.
	ErrInvalidPublicKeyPEM = errors.New("public key must be a PEM-encoded RSA public key of at least 2048 bits")
)

// APIKeyRecord is the persisted (non-secret) form of an API key. ID doubles
// as the JWT `kid` a caller must set to authenticate as this key — there is
// no separate secret or hash: the server only ever stores the caller's
// public key (see CreateAPIKey), never a private key.
type APIKeyRecord struct {
	ID         uuid.UUID  `json:"id"`
	Username   string     `json:"username"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"createdAt"`
	CreatedBy  string     `json:"createdBy"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

// validateRSAPublicKeyPEM parses pemStr as a PKIX-encoded RSA public key
// (the format produced by GenerateKeyPairHandler and x509.MarshalPKIXPublicKey
// generally) and rejects anything under minRSAKeyBits.
func validateRSAPublicKeyPEM(pemStr string) error {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return ErrInvalidPublicKeyPEM
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidPublicKeyPEM, err)
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok || rsaPub.N.BitLen() < minRSAKeyBits {
		return ErrInvalidPublicKeyPEM
	}

	return nil
}

// CreateAPIKey registers publicKeyPEM (caller-generated, >=2048-bit RSA,
// PKIX-encoded PEM) as a new API key for username, labeled name. The server
// never sees a private key: the caller alone is responsible for signing JWTs
// with the matching private key and presenting them as bearer tokens (see
// ResolveAPIKeySigningKey). It returns ErrInvalidPublicKeyPEM if publicKeyPEM
// doesn't parse as required, or ErrUserNotFound if username doesn't exist.
func (s *Store) CreateAPIKey(ctx context.Context, username, name, createdBy, publicKeyPEM string) (APIKeyRecord, error) {
	if err := validateRSAPublicKeyPEM(publicKeyPEM); err != nil {
		return APIKeyRecord{}, err
	}

	rec := APIKeyRecord{
		ID:        uuid.New(),
		Username:  username,
		Name:      name,
		CreatedBy: createdBy,
	}

	err := s.pool.QueryRow(ctx, `
		INSERT INTO api_keys (id, public_key_pem, username, name, created_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at
	`, rec.ID, publicKeyPEM, username, name, createdBy).Scan(&rec.CreatedAt)
	if err != nil {
		if isPgErrCode(err, "23503") {
			return APIKeyRecord{}, ErrUserNotFound
		}

		return APIKeyRecord{}, fmt.Errorf("create api key: %w", err)
	}

	return rec, nil
}

// ListAPIKeys returns every non-revoked API key belonging to username, most
// recently created first.
func (s *Store) ListAPIKeys(ctx context.Context, username string) ([]APIKeyRecord, error) {
	return collectRows(ctx, s.pool, "list api keys", `
		SELECT id, username, name, created_at, created_by, last_used_at
		FROM api_keys
		WHERE username = $1 AND revoked_at IS NULL
		ORDER BY created_at DESC
	`, func(rows pgx.Rows) (APIKeyRecord, error) {
		var r APIKeyRecord

		err := rows.Scan(&r.ID, &r.Username, &r.Name, &r.CreatedAt, &r.CreatedBy, &r.LastUsedAt)

		return r, err
	}, username)
}

// RevokeAPIKey revokes id, if it belongs to username and isn't already
// revoked. It returns ErrAPIKeyNotFound otherwise.
func (s *Store) RevokeAPIKey(ctx context.Context, username string, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE api_keys SET revoked_at = now()
		WHERE id = $1 AND username = $2 AND revoked_at IS NULL
	`, id, username)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}

	return nil
}

// apiKeySigningKey is the cached form of a resolved key: the username it
// authenticates as, plus its registered public key PEM (parsing that PEM
// into an *rsa.PublicKey happens in internal/handler, which owns all JWT
// verification concerns and already depends on golang-jwt).
type apiKeySigningKey struct {
	username     string
	publicKeyPEM string
}

// ResolveAPIKeySigningKey resolves keyID — a JWT's `kid` header, which IS
// api_keys.id — to the username it authenticates as and its registered
// public key PEM. It returns ErrInvalidAPIKey if keyID is unknown or
// revoked. Results are cached for cacheTTL, keyed by id, since this runs on
// every API-key-authenticated request.
func (s *Store) ResolveAPIKeySigningKey(ctx context.Context, keyID uuid.UUID) (username, publicKeyPEM string, err error) {
	if v, ok := s.apiKeyCache.get(keyID); ok {
		return v.username, v.publicKeyPEM, nil
	}

	err = s.pool.QueryRow(ctx, `
		SELECT username, public_key_pem FROM api_keys WHERE id = $1 AND revoked_at IS NULL
	`, keyID).Scan(&username, &publicKeyPEM)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrInvalidAPIKey
	}

	if err != nil {
		return "", "", fmt.Errorf("resolve api key signing key: %w", err)
	}

	s.apiKeyCache.set(keyID, apiKeySigningKey{username: username, publicKeyPEM: publicKeyPEM})
	s.TouchAPIKeyLastUsed(ctx, keyID)

	return username, publicKeyPEM, nil
}

// TouchAPIKeyLastUsed best-effort records that id was just used, logging
// (rather than propagating) any failure: it's observability, not a
// correctness requirement, so it must never fail an authenticated request.
func (s *Store) TouchAPIKeyLastUsed(ctx context.Context, id uuid.UUID) {
	if _, err := s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at = now() WHERE id = $1`, id); err != nil {
		log.Printf("touch api key last_used_at for %s: %v", id, err)
	}
}
