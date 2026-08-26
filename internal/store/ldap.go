package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// FindUserByLDAPIdentity returns the username of the local account linked to
// the given LDAP entry DN (see CreateLDAPUser), or ErrUserNotFound if no
// account is linked yet.
func (s *Store) FindUserByLDAPIdentity(ctx context.Context, dn string) (string, error) {
	var username string

	err := s.pool.QueryRow(ctx, `
		SELECT username FROM users WHERE ldap_dn = $1
	`, dn).Scan(&username)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUserNotFound
	}

	if err != nil {
		return "", fmt.Errorf("find user by ldap identity: %w", err)
	}

	return username, nil
}

// CreateLDAPUser auto-provisions a local account for a first-time login at
// the given LDAP entry DN, linking it so a later login at the same identity
// resolves back to it via FindUserByLDAPIdentity. It starts with no
// permissions at all (can_create/edit/delete and is_admin all false) — least
// privilege for an identity nobody has vetted; an admin grants whatever
// access is appropriate afterward via the existing /users API. Its
// password_hash is set to a random value never handed back to anyone, so the
// account has no usable local password of its own; it can only ever sign in
// via LDAP.
//
// preferredUsername is tried as-is first; if it's already taken by some
// other account, a short random suffix is appended and creation is retried
// once so provisioning doesn't fail just because the name collides.
func (s *Store) CreateLDAPUser(ctx context.Context, preferredUsername, dn string) (UserRecord, error) {
	randomPassword, err := newRefreshTokenValue()
	if err != nil {
		return UserRecord{}, err
	}

	hash, err := hashPassword(randomPassword)
	if err != nil {
		return UserRecord{}, err
	}

	u, err := s.insertLDAPUser(ctx, preferredUsername, dn, hash)
	if err == nil {
		return u, nil
	}

	if !isPgErrCode(err, "23505") {
		return UserRecord{}, fmt.Errorf("create ldap user: %w", err)
	}

	suffix, err := randomUsernameSuffix()
	if err != nil {
		return UserRecord{}, err
	}

	u, err = s.insertLDAPUser(ctx, preferredUsername+"-"+suffix, dn, hash)
	if err != nil {
		return UserRecord{}, fmt.Errorf("create ldap user: %w", err)
	}

	return u, nil
}

func (s *Store) insertLDAPUser(ctx context.Context, username, dn, passwordHash string) (UserRecord, error) {
	u := UserRecord{Username: username}

	const noPermissions = false

	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (username, password_hash, can_create, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, is_admin, ldap_dn)
		VALUES ($1, $2, $3, $3, $3, $3, $3, $3, $4)
		RETURNING created_at
	`, username, passwordHash, noPermissions, dn).Scan(&u.CreatedAt)
	if err != nil {
		return UserRecord{}, err
	}

	return u, nil
}
