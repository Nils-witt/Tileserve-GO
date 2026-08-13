package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// APIKeyPrefix distinguishes an API key from a JWT at a glance, so
// parseBearerToken (internal/handler/auth.go) can dispatch on it without
// trial-parsing a JWT first.
const APIKeyPrefix = "tsk_"

// apiKeyBytes is how much crypto/rand entropy backs each issued API key
// before base64 encoding, matching refreshTokenBytes' precedent.
const apiKeyBytes = 32

var (
	// ErrInvalidAPIKey is returned when an API key is unknown or revoked.
	ErrInvalidAPIKey = errors.New("invalid or revoked api key")
	// ErrAPIKeyNotFound is returned when looking up a specific key by id finds no row.
	ErrAPIKeyNotFound = errors.New("api key not found")
)

// APIKeyRecord is the persisted (non-secret) form of an API key: everything
// but the key itself, which is only ever returned once, at creation.
type APIKeyRecord struct {
	ID         uuid.UUID  `json:"id"`
	Username   string     `json:"username"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"createdAt"`
	CreatedBy  string     `json:"createdBy"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

// newAPIKeyValue returns a random, URL-safe API key string, prefixed with
// APIKeyPrefix.
func newAPIKeyValue() (string, error) {
	buf := make([]byte, apiKeyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate api key: %w", err)
	}

	return APIKeyPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashAPIKey returns the hex-encoded SHA-256 digest of key. Only this digest
// is ever persisted, matching hashRefreshToken's rationale: SHA-256 (rather
// than bcrypt) is fine here because the input is high-entropy random data,
// not a low-entropy user password.
func hashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// CreateAPIKey issues a new API key for username, labeled name. It returns
// ErrUserNotFound if username doesn't exist. The plaintext key is returned
// only here — it is never recoverable again, only its hash is stored.
func (s *Store) CreateAPIKey(ctx context.Context, username, name, createdBy string) (plainKey string, rec APIKeyRecord, err error) {
	plainKey, err = newAPIKeyValue()
	if err != nil {
		return "", APIKeyRecord{}, err
	}

	rec = APIKeyRecord{
		ID:        uuid.New(),
		Username:  username,
		Name:      name,
		CreatedBy: createdBy,
	}

	err = s.pool.QueryRow(ctx, `
		INSERT INTO api_keys (id, key_hash, username, name, created_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at
	`, rec.ID, hashAPIKey(plainKey), username, name, createdBy).Scan(&rec.CreatedAt)
	if err != nil {
		if isPgErrCode(err, "23503") {
			return "", APIKeyRecord{}, ErrUserNotFound
		}

		return "", APIKeyRecord{}, fmt.Errorf("create api key: %w", err)
	}

	return plainKey, rec, nil
}

// ListAPIKeys returns every non-revoked API key belonging to username, most
// recently created first. The plaintext key is never included (it isn't
// stored).
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

// LookupAPIKey resolves plainKey (as presented in a bearer token) to the
// username it authenticates as. It returns ErrInvalidAPIKey if the key is
// unknown or revoked. Results are cached for cacheTTL, keyed by hash rather
// than plaintext, since this is called on every API-key-authenticated
// request.
func (s *Store) LookupAPIKey(ctx context.Context, plainKey string) (string, error) {
	hash := hashAPIKey(plainKey)

	if username, ok := s.apiKeyCache.get(hash); ok {
		return username, nil
	}

	var (
		id       uuid.UUID
		username string
	)

	err := s.pool.QueryRow(ctx, `
		SELECT id, username FROM api_keys WHERE key_hash = $1 AND revoked_at IS NULL
	`, hash).Scan(&id, &username)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidAPIKey
	}

	if err != nil {
		return "", fmt.Errorf("look up api key: %w", err)
	}

	s.apiKeyCache.set(hash, username)
	s.TouchAPIKeyLastUsed(ctx, id)

	return username, nil
}

// TouchAPIKeyLastUsed best-effort records that id was just used, logging
// (rather than propagating) any failure: it's observability, not a
// correctness requirement, so it must never fail an authenticated request.
func (s *Store) TouchAPIKeyLastUsed(ctx context.Context, id uuid.UUID) {
	if _, err := s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at = now() WHERE id = $1`, id); err != nil {
		log.Printf("touch api key last_used_at for %s: %v", id, err)
	}
}
