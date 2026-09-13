package store

import (
	"context"
	"crypto/rand"
	"crypto/sha3"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ErrInvalidRefreshToken is returned when a refresh token is unknown, expired, or revoked.
var ErrInvalidRefreshToken = errors.New("invalid refresh token")

// refreshTokenModel is the GORM-mapped form of one row in refresh_tokens —
// there's no public record type for it since a refresh token's value is
// only ever returned as a bare string, never a persisted-row shape.
type refreshTokenModel struct {
	TokenHash string     `gorm:"column:token_hash;primaryKey"`
	Username  string     `gorm:"column:username;not null"`
	ExpiresAt time.Time  `gorm:"column:expires_at;not null"`
	CreatedAt time.Time  `gorm:"column:created_at;not null;default:now()"`
	RevokedAt *time.Time `gorm:"column:revoked_at"`
}

func (refreshTokenModel) TableName() string { return "refresh_tokens" }

// refreshTokenBytes is how much crypto/rand entropy backs each issued
// refresh token before base64 encoding.
const refreshTokenBytes = 32

// newRefreshTokenValue returns a random, URL-safe refresh token string.
func newRefreshTokenValue() (string, error) {
	buf := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate refresh token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashRefreshToken returns the hex-encoded SHA3-256 digest of token. Only
// this digest is ever persisted, so a database leak alone doesn't hand out
// usable refresh tokens; a fast hash (rather than bcrypt) is fine here
// because the input is high-entropy random data, not a low-entropy user
// password, and it must support exact-match lookup in the database.
func hashRefreshToken(token string) string {
	sum := sha3.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateRefreshToken issues and stores a new refresh token for username,
// valid for ttl.
func (s *Store) CreateRefreshToken(ctx context.Context, username string, ttl time.Duration) (token string, expiresAt time.Time, err error) {
	token, err = newRefreshTokenValue()
	if err != nil {
		return "", time.Time{}, err
	}

	expiresAt = time.Now().Add(ttl)

	rt := refreshTokenModel{TokenHash: hashRefreshToken(token), Username: username, ExpiresAt: expiresAt}
	if err := s.db.WithContext(ctx).Create(&rt).Error; err != nil {
		return "", time.Time{}, fmt.Errorf("create refresh token: %w", err)
	}

	return token, expiresAt, nil
}

// RotateRefreshToken redeems oldToken for a new refresh token belonging to
// the same user: the old token is revoked and a new one issued in the same
// transaction, so each refresh token is single-use. It returns
// ErrInvalidRefreshToken if oldToken is unknown, expired, or already
// revoked — the last case includes reuse of a previously-rotated token,
// which signals the token was stolen rather than a legitimate double-use.
func (s *Store) RotateRefreshToken(ctx context.Context, oldToken string, ttl time.Duration) (username, newToken string, expiresAt time.Time, err error) {
	oldHash := hashRefreshToken(oldToken)

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var oldExpiresAt time.Time

		scanErr := tx.Raw(`
			SELECT username, expires_at FROM refresh_tokens
			WHERE token_hash = ? AND revoked_at IS NULL
			FOR UPDATE
		`, oldHash).Row().Scan(&username, &oldExpiresAt)
		if errors.Is(scanErr, sql.ErrNoRows) {
			return ErrInvalidRefreshToken
		}

		if scanErr != nil {
			return fmt.Errorf("look up refresh token: %w", scanErr)
		}

		if time.Now().After(oldExpiresAt) {
			return ErrInvalidRefreshToken
		}

		if err := tx.Model(&refreshTokenModel{}).Where("token_hash = ?", oldHash).Update("revoked_at", gorm.Expr("now()")).Error; err != nil {
			return fmt.Errorf("revoke refresh token: %w", err)
		}

		newToken, err = newRefreshTokenValue()
		if err != nil {
			return err
		}

		expiresAt = time.Now().Add(ttl)

		rt := refreshTokenModel{TokenHash: hashRefreshToken(newToken), Username: username, ExpiresAt: expiresAt}
		if err := tx.Create(&rt).Error; err != nil {
			return fmt.Errorf("insert rotated refresh token: %w", err)
		}

		return nil
	})
	if err != nil {
		if errors.Is(err, ErrInvalidRefreshToken) {
			return "", "", time.Time{}, ErrInvalidRefreshToken
		}

		return "", "", time.Time{}, err
	}

	return username, newToken, expiresAt, nil
}
