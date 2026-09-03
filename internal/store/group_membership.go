package store

import (
	"context"
	"fmt"
)

// SyncGroupMembershipByLDAPDNs replaces username's group memberships with
// exactly the groups whose ldap_group_dn appears in dns, in a single
// transaction so a partial sync is never observed. Called on every
// successful LDAP login (see the auth/ldap package) since a user's
// directory group membership can change between logins and must stay in
// sync — group membership is never locally/manually managed.
func (s *Store) SyncGroupMembershipByLDAPDNs(ctx context.Context, username string, dns []string) error {
	return s.syncGroupMembership(ctx, username, "ldap_group_dn", dns)
}

// SyncGroupMembershipByOIDCClaims is SyncGroupMembershipByLDAPDNs' OIDC
// equivalent, matching claimValues (the ID token's "groups" claim) against
// groups.oidc_group_claim. Called on every successful OIDC login (see the
// auth/oidc package).
func (s *Store) SyncGroupMembershipByOIDCClaims(ctx context.Context, username string, claimValues []string) error {
	return s.syncGroupMembership(ctx, username, "oidc_group_claim", claimValues)
}

// syncGroupMembership fully replaces username's group_members rows with the
// groups whose idColumn value appears in values, inside one transaction.
// values may be empty — a user with no matching directory groups ends up in
// no groups at all, since this is a full resync, not an incremental add.
// idColumn is always one of the two fixed literal strings passed by the
// wrappers above, never derived from request input, so interpolating it
// into the query text carries the same injection safety as a fixed
// placeholder (see queryBuilder's doc comment for the same argument).
func (s *Store) syncGroupMembership(ctx context.Context, username, idColumn string, values []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("sync group membership: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM group_members WHERE username = $1`, username); err != nil {
		return fmt.Errorf("sync group membership: clear existing: %w", err)
	}

	if len(values) > 0 {
		query := fmt.Sprintf(`
			INSERT INTO group_members (group_id, username)
			SELECT id, $1 FROM groups WHERE %s = ANY($2) AND %s <> ''
		`, idColumn, idColumn)

		if _, err := tx.Exec(ctx, query, username, values); err != nil {
			return fmt.Errorf("sync group membership: insert: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("sync group membership: commit: %w", err)
	}

	s.permsCache.invalidate(username)
	// mapPermCache entries for (mapID, username) aren't individually
	// invalidated here (unlike SetMapPermission's targeted invalidation) —
	// there's no cheap way to enumerate every map a newly-synced group
	// grants access to. Accept up to cacheTTL staleness for group-derived
	// per-map grants right after a login, consistent with this cache's
	// existing tolerance elsewhere.

	return nil
}
