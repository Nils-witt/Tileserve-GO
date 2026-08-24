package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// FindUserByOIDCIdentity returns the username of the local account linked to
// the given OIDC issuer+subject pair (see CreateOIDCUser), or
// ErrUserNotFound if no account is linked yet.
func (s *Store) FindUserByOIDCIdentity(ctx context.Context, issuer, subject string) (string, error) {
	var username string

	err := s.pool.QueryRow(ctx, `
		SELECT username FROM users WHERE oidc_issuer = $1 AND oidc_subject = $2
	`, issuer, subject).Scan(&username)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUserNotFound
	}

	if err != nil {
		return "", fmt.Errorf("find user by oidc identity: %w", err)
	}

	return username, nil
}

// CreateOIDCUser auto-provisions a local account for a first-time login at
// the given OIDC issuer+subject, linking it so a later login at the same
// identity resolves back to it via FindUserByOIDCIdentity. It starts with no
// permissions at all (can_create/edit/delete and is_admin all false) — least
// privilege for an identity nobody has vetted; an admin grants whatever
// access is appropriate afterward via the existing /users API. Its
// password_hash is set to a random value never handed back to anyone, so the
// account has no usable password of its own; it can only ever sign in via
// OIDC.
//
// preferredUsername is tried as-is first; if it's already taken by some
// other account, a short random suffix is appended and creation is retried
// once so provisioning doesn't fail just because the name collides.
func (s *Store) CreateOIDCUser(ctx context.Context, preferredUsername, cn, issuer, subject string) (UserRecord, error) {
	randomPassword, err := newRefreshTokenValue()
	if err != nil {
		return UserRecord{}, err
	}

	hash, err := hashPassword(randomPassword)
	if err != nil {
		return UserRecord{}, err
	}

	u, err := s.insertOIDCUser(ctx, preferredUsername, cn, issuer, subject, hash)
	if err == nil {
		return u, nil
	}

	if !isPgErrCode(err, "23505") {
		return UserRecord{}, fmt.Errorf("create oidc user: %w", err)
	}

	suffix, err := randomUsernameSuffix()
	if err != nil {
		return UserRecord{}, err
	}

	u, err = s.insertOIDCUser(ctx, preferredUsername+"-"+suffix, cn, issuer, subject, hash)
	if err != nil {
		return UserRecord{}, fmt.Errorf("create oidc user: %w", err)
	}

	return u, nil
}

func (s *Store) insertOIDCUser(ctx context.Context, username, cn, issuer, subject, passwordHash string) (UserRecord, error) {
	u := UserRecord{Username: username, CN: cn}

	const noPermissions = false

	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (username, password_hash, cn, can_create, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, is_admin, oidc_issuer, oidc_subject)
		VALUES ($1, $2, $3, $4, $4, $4, $4, $4, $4, $5, $6)
		RETURNING created_at
	`, username, passwordHash, cn, noPermissions, issuer, subject).Scan(&u.CreatedAt)
	if err != nil {
		return UserRecord{}, err
	}

	return u, nil
}

// randomUsernameSuffix returns a short random hex string used to disambiguate
// an auto-provisioned OIDC username that collides with an existing account.
func randomUsernameSuffix() (string, error) {
	buf := make([]byte, 3)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate username suffix: %w", err)
	}

	return hex.EncodeToString(buf), nil
}
