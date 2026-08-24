package handler

import (
	"context"
	"errors"

	"nilswitt.dev/tileserve-go/internal/ldapauth"
	"nilswitt.dev/tileserve-go/internal/store"
)

// authenticatePassword verifies username/password for POST /login: it tries
// the local store first, and only falls back to LDAP (when ldapAuth is
// configured, i.e. non-nil — see *OIDCAuthenticator for the same
// nil-means-disabled convention) if that account is unknown or its password
// doesn't match — an existing local account's password is never shadowed by
// a directory lookup. A successful LDAP bind resolves to a local account via
// resolveLDAPUsername, auto-provisioning one on first login; the returned
// username is the one to issue a session for, which may differ from the
// caller-supplied username if provisioning had to disambiguate a collision.
func authenticatePassword(ctx context.Context, st *store.Store, ldapAuth *ldapauth.Authenticator, username, password string) (string, error) {
	err := st.Authenticate(ctx, username, password)
	if err == nil {
		return username, nil
	}

	if !errors.Is(err, store.ErrInvalidCredentials) {
		return "", err
	}

	if ldapAuth == nil {
		return "", store.ErrInvalidCredentials
	}

	identity, err := ldapAuth.Authenticate(ctx, username, password)
	if err != nil {
		return "", store.ErrInvalidCredentials
	}

	return resolveLDAPUsername(ctx, st, username, identity)
}

// resolveLDAPUsername resolves identity's DN to a local account, auto-
// provisioning one via store.CreateLDAPUser on first login at that identity.
func resolveLDAPUsername(ctx context.Context, st *store.Store, preferredUsername string, identity ldapauth.Identity) (string, error) {
	username, err := st.FindUserByLDAPIdentity(ctx, identity.DN)
	if err == nil {
		return username, nil
	}

	if !errors.Is(err, store.ErrUserNotFound) {
		return "", err
	}

	candidate := firstNonEmpty(preferredUsername, identity.CN, identity.DN)

	u, err := st.CreateLDAPUser(ctx, candidate, identity.CN, identity.DN)
	if err != nil {
		return "", err
	}

	return u.Username, nil
}
