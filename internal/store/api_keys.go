package store

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
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
	ID           uuid.UUID  `json:"id" gorm:"column:id;type:uuid;primaryKey"`
	PublicKeyPEM string     `json:"-" gorm:"column:public_key_pem;not null;default:''"`
	Username     string     `json:"username" gorm:"column:username;not null"`
	Name         string     `json:"name" gorm:"column:name;not null;default:''"`
	CreatedAt    time.Time  `json:"createdAt" gorm:"column:created_at;not null;default:now()"`
	CreatedBy    string     `json:"createdBy" gorm:"column:created_by;not null"`
	LastUsedAt   *time.Time `json:"lastUsedAt,omitempty" gorm:"column:last_used_at"`
	RevokedAt    *time.Time `json:"-" gorm:"column:revoked_at"`
	// Scoped reports whether this key is restricted to a subset of maps/
	// versions (see api_key_scopes.go). A key with no scopes set is
	// unrestricted regardless of this flag; Scoped only ever narrows access
	// once true, it never widens it.
	Scoped bool `json:"scoped" gorm:"column:scoped;not null;default:false"`
}

// TableName implements the gorm.Tabler interface.
func (APIKeyRecord) TableName() string { return "api_keys" }

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
		ID:           uuid.New(),
		PublicKeyPEM: publicKeyPEM,
		Username:     username,
		Name:         name,
		CreatedBy:    createdBy,
	}

	if err := s.db.WithContext(ctx).Create(&rec).Error; err != nil {
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
	keys := []APIKeyRecord{}

	err := s.db.WithContext(ctx).
		Where("username = ? AND revoked_at IS NULL", username).
		Order("created_at DESC").
		Find(&keys).Error
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}

	return keys, nil
}

// RevokeAPIKey revokes id, if it belongs to username and isn't already
// revoked. It returns ErrAPIKeyNotFound otherwise.
func (s *Store) RevokeAPIKey(ctx context.Context, username string, id uuid.UUID) error {
	res := s.db.WithContext(ctx).Model(&APIKeyRecord{}).
		Where("id = ? AND username = ? AND revoked_at IS NULL", id, username).
		Update("revoked_at", gorm.Expr("now()"))
	if res.Error != nil {
		return fmt.Errorf("revoke api key: %w", res.Error)
	}

	if res.RowsAffected == 0 {
		return ErrAPIKeyNotFound
	}

	return nil
}

// apiKeySigningKey is the cached form of a resolved key: the username it
// authenticates as, its registered public key PEM (parsing that PEM into an
// *rsa.PublicKey happens in internal/webserver, which owns all JWT
// verification concerns and already depends on golang-jwt), and whether it's
// scope-restricted (see api_key_scopes.go) — cached alongside the rest since
// ResolveAPIKeySigningKey already fetches the row on every API-key-
// authenticated request, letting apiKeyScopedFlag reuse this cache entry
// instead of a second round trip.
type apiKeySigningKey struct {
	username     string
	publicKeyPEM string
	scoped       bool
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

	var scoped bool

	err = s.db.WithContext(ctx).Raw(`
		SELECT username, public_key_pem, scoped FROM api_keys WHERE id = ? AND revoked_at IS NULL
	`, keyID).Row().Scan(&username, &publicKeyPEM, &scoped)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrInvalidAPIKey
	}

	if err != nil {
		return "", "", fmt.Errorf("resolve api key signing key: %w", err)
	}

	s.apiKeyCache.set(keyID, apiKeySigningKey{username: username, publicKeyPEM: publicKeyPEM, scoped: scoped})
	s.TouchAPIKeyLastUsed(ctx, keyID)

	return username, publicKeyPEM, nil
}

// apiKeyScopedFlag reports whether apiKeyID is scope-restricted, reusing the
// apiKeyCache entry ResolveAPIKeySigningKey populates on every API-key-
// authenticated request when available, falling back to a direct lookup
// otherwise. It returns ErrInvalidAPIKey if keyID is unknown or revoked.
func (s *Store) apiKeyScopedFlag(ctx context.Context, apiKeyID uuid.UUID) (bool, error) {
	if v, ok := s.apiKeyCache.get(apiKeyID); ok {
		return v.scoped, nil
	}

	var scoped bool

	err := s.db.WithContext(ctx).Raw(`SELECT scoped FROM api_keys WHERE id = ? AND revoked_at IS NULL`, apiKeyID).Row().Scan(&scoped)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrInvalidAPIKey
	}

	if err != nil {
		return false, fmt.Errorf("get api key scoped flag: %w", err)
	}

	return scoped, nil
}

// TouchAPIKeyLastUsed best-effort records that id was just used, logging
// (rather than propagating) any failure: it's observability, not a
// correctness requirement, so it must never fail an authenticated request.
func (s *Store) TouchAPIKeyLastUsed(ctx context.Context, id uuid.UUID) {
	err := s.db.WithContext(ctx).Exec(`UPDATE api_keys SET last_used_at = now() WHERE id = ?`, id).Error
	if err != nil {
		log.Printf("touch api key last_used_at for %s: %v", id, err)
	}
}
