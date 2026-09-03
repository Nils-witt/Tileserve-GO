package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SyncGroupMembershipByLDAPDNs replaces username's group memberships with
// exactly the groups whose ldap_group_dn appears in dns, auto-creating a
// group for any dn that doesn't match one yet (see getOrCreateGroupByLink).
// Called on every successful LDAP login (see the auth/ldap package) since a
// user's directory group membership can change between logins and must stay
// in sync — group membership is never locally/manually managed.
func (s *Store) SyncGroupMembershipByLDAPDNs(ctx context.Context, username string, dns []string) error {
	return s.syncGroupMembership(ctx, username, "ldap_group_dn", "ldap-sync", dns)
}

// SyncGroupMembershipByOIDCClaims is SyncGroupMembershipByLDAPDNs' OIDC
// equivalent, matching claimValues (the ID token's "groups" claim) against
// groups.oidc_group_claim. Called on every successful OIDC login (see the
// auth/oidc package).
func (s *Store) SyncGroupMembershipByOIDCClaims(ctx context.Context, username string, claimValues []string) error {
	return s.syncGroupMembership(ctx, username, "oidc_group_claim", "oidc-sync", claimValues)
}

// syncGroupMembership fully replaces username's group_members rows with the
// groups linked (via idColumn) to each distinct, non-empty value — creating
// one for any value that isn't linked to an existing group yet. values may
// be empty — a user with no directory groups ends up in no groups at all,
// since this is a full resync, not an incremental add. idColumn is always
// one of the two fixed literal strings passed by the wrappers above, never
// derived from request input, so interpolating it into query text carries
// the same injection safety as a fixed placeholder (see queryBuilder's doc
// comment for the same argument). Group resolution/creation happens before
// the membership-replacing transaction below (rather than inside it) so a
// name-collision retry (see getOrCreateGroupByLink) never has to unwind a
// partially-aborted transaction.
func (s *Store) syncGroupMembership(ctx context.Context, username, idColumn, actor string, values []string) error {
	ids := make([]uuid.UUID, 0, len(values))
	seen := make(map[string]bool, len(values))

	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}

		seen[v] = true

		id, err := s.getOrCreateGroupByLink(ctx, idColumn, v, actor)
		if err != nil {
			return fmt.Errorf("sync group membership: resolve group for %s=%q: %w", idColumn, v, err)
		}

		ids = append(ids, id)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("sync group membership: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM group_members WHERE username = $1`, username); err != nil {
		return fmt.Errorf("sync group membership: clear existing: %w", err)
	}

	for _, id := range ids {
		if _, err := tx.Exec(ctx, `INSERT INTO group_members (group_id, username) VALUES ($1, $2)`, id, username); err != nil {
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

// getOrCreateGroupByLink resolves the group linked to value via idColumn
// ("ldap_group_dn" or "oidc_group_claim"), auto-creating one — named per
// groupDisplayName, with every permission flag false and no per-map grants —
// the first time a login references a directory/IdP group this server
// hasn't seen before. This mirrors CreateLDAPUser/CreateOIDCUser's "least
// privilege; an admin grants whatever access is appropriate afterward"
// precedent: the group exists and its members are tracked, but it grants
// nothing until an admin opts it into some capability via PUT /groups/{id}.
// actor (e.g. "ldap-sync"/"oidc-sync") is recorded in created_by/updated_by
// so an admin can tell an auto-created group apart from one they made
// themselves — not a real username, same as audit_logs.actor isn't
// constrained to be one.
func (s *Store) getOrCreateGroupByLink(ctx context.Context, idColumn, value, actor string) (uuid.UUID, error) {
	id, err := s.findGroupByLink(ctx, idColumn, value)
	if err == nil {
		return id, nil
	}

	if !errors.Is(err, ErrGroupNotFound) {
		return uuid.UUID{}, err
	}

	name := groupDisplayName(idColumn, value)

	id, err = s.insertSyncedGroup(ctx, idColumn, value, name, actor)
	if err == nil {
		return id, nil
	}

	if !isPgErrCode(err, "23505") {
		return uuid.UUID{}, fmt.Errorf("create group: %w", err)
	}

	// Unique violation on the insert above is ambiguous: either a
	// concurrent sync just linked idColumn to value first (recheck and use
	// it), or name collides with some other, differently-linked group's
	// existing name (retry once with a random suffix, same disambiguation
	// CreateOIDCUser/CreateLDAPUser use for a colliding username).
	if id, err := s.findGroupByLink(ctx, idColumn, value); err == nil {
		return id, nil
	}

	suffix, err := randomUsernameSuffix()
	if err != nil {
		return uuid.UUID{}, err
	}

	id, err = s.insertSyncedGroup(ctx, idColumn, value, name+"-"+suffix, actor)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("create group: %w", err)
	}

	return id, nil
}

// groupDisplayName picks the name an auto-created group gets for value
// linked via idColumn. For an LDAP DN this is the value of its leading
// "cn=" component (e.g. "editors" for
// "cn=editors,ou=groups,dc=example,dc=com"), falling back to the full dn if
// it isn't shaped like "cn=..." — a display convenience only: value itself,
// not this derived name, is what SyncGroupMembershipByLDAPDNs matches
// against (see ldap_group_dn). An OIDC claim value is already a short,
// human-chosen name, so it's used as-is.
func groupDisplayName(idColumn, value string) string {
	if idColumn != "ldap_group_dn" {
		return value
	}

	first, _, _ := strings.Cut(value, ",")

	attr, name, ok := strings.Cut(first, "=")
	if !ok || !strings.EqualFold(strings.TrimSpace(attr), "cn") {
		return value
	}

	return strings.TrimSpace(name)
}

// findGroupByLink returns the id of the group linked to value via idColumn,
// or ErrGroupNotFound if none is.
func (s *Store) findGroupByLink(ctx context.Context, idColumn, value string) (uuid.UUID, error) {
	var id uuid.UUID

	err := s.pool.QueryRow(ctx, fmt.Sprintf(`SELECT id FROM groups WHERE %s = $1`, idColumn), value).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.UUID{}, ErrGroupNotFound
	}

	if err != nil {
		return uuid.UUID{}, fmt.Errorf("look up group by %s: %w", idColumn, err)
	}

	return id, nil
}

// insertSyncedGroup creates a new, all-permissions-false group named name,
// linked to value via idColumn.
func (s *Store) insertSyncedGroup(ctx context.Context, idColumn, value, name, actor string) (uuid.UUID, error) {
	id := uuid.New()

	query := fmt.Sprintf(`
		INSERT INTO groups (id, name, %s, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $4)
	`, idColumn)

	if _, err := s.pool.Exec(ctx, query, id, name, value, actor); err != nil {
		return uuid.UUID{}, err
	}

	return id, nil
}
