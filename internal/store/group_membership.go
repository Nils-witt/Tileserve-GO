package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// groupMemberModel is the GORM-mapped form of one row in group_members —
// there's no public record type for it since membership is never read back
// as a standalone entity (see GetPermissions/GetMapPermission's joins
// instead).
type groupMemberModel struct {
	GroupID  uuid.UUID `gorm:"column:group_id;type:uuid;primaryKey"`
	Username string    `gorm:"column:username;primaryKey"`
	SyncedAt time.Time `gorm:"column:synced_at;not null;default:now()"`
}

func (groupMemberModel) TableName() string { return "group_members" }

// SyncGroupMembershipByLDAPDNs replaces username's group memberships with
// exactly the groups whose ldap_group_dn appears in dns, auto-creating a
// group for any dn that doesn't match one yet (see getOrCreateGroupByLink).
// Called on every successful LDAP login (see the auth/ldap package) since a
// user's directory group membership can change between logins and must stay
// in sync — group membership is never locally/manually managed.
func (s *Store) SyncGroupMembershipByLDAPDNs(ctx context.Context, username string, dns []string) error {
	return s.syncGroupMembership(ctx, username, dns, "ldap_group_dn", s.getOrCreateGroupByLDAPDN)
}

// SyncGroupMembershipByOIDCClaims is SyncGroupMembershipByLDAPDNs' OIDC
// equivalent, matching claimValues (the ID token's "groups" claim) against
// groups.oidc_group_claim. Called on every successful OIDC login (see the
// auth/oidc package).
func (s *Store) SyncGroupMembershipByOIDCClaims(ctx context.Context, username string, claimValues []string) error {
	return s.syncGroupMembership(ctx, username, claimValues, "oidc_group_claim", s.getOrCreateGroupByOIDCClaim)
}

// syncGroupMembership is the shared implementation behind
// SyncGroupMembershipByLDAPDNs and SyncGroupMembershipByOIDCClaims: it
// resolves each of values (deduplicated, blanks skipped) to a group id via
// resolve, then replaces username's membership with exactly those groups.
// label names the source column (e.g. "ldap_group_dn") for error messages.
func (s *Store) syncGroupMembership(ctx context.Context, username string, values []string, label string, resolve func(context.Context, string) (uuid.UUID, error)) error {
	ids := make([]uuid.UUID, 0, len(values))
	seen := make(map[string]bool, len(values))

	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}

		seen[v] = true

		id, err := resolve(ctx, v)
		if err != nil {
			return fmt.Errorf("sync group membership: resolve group for %s=%q: %w", label, v, err)
		}

		ids = append(ids, id)
	}

	return s.replaceGroupMembership(ctx, username, ids)
}

// replaceGroupMembership fully replaces username's group_members rows with
// ids. values may be empty — a user with no directory groups ends up in no
// groups at all, since this is a full resync, not an incremental add. Group
// resolution/creation happens before this replacing transaction (rather
// than inside it) so a name-collision retry (see getOrCreateGroupByLDAPDN/
// getOrCreateGroupByOIDCClaim) never has to unwind a partially-aborted
// transaction.
func (s *Store) replaceGroupMembership(ctx context.Context, username string, ids []uuid.UUID) error {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("username = ?", username).Delete(&groupMemberModel{}).Error; err != nil {
			return fmt.Errorf("sync group membership: clear existing: %w", err)
		}

		for _, id := range ids {
			if err := tx.Create(&groupMemberModel{GroupID: id, Username: username}).Error; err != nil {
				return fmt.Errorf("sync group membership: insert: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return err
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

// getOrCreateGroupByLDAPDN resolves the group linked to dn via
// ldap_group_dn, auto-creating one the first time a login references a
// directory group this server hasn't seen before — see
// getOrCreateGroupByLink's doc comment for the shared reasoning.
func (s *Store) getOrCreateGroupByLDAPDN(ctx context.Context, dn string) (uuid.UUID, error) {
	return s.getOrCreateGroupByLink(ctx, "ldap_group_dn", dn, ldapGroupDisplayName(dn), "ldap-sync")
}

// getOrCreateGroupByOIDCClaim resolves the group linked to claimValue via
// oidc_group_claim, auto-creating one the first time a login references an
// IdP group this server hasn't seen before — see getOrCreateGroupByLink's
// doc comment for the shared reasoning.
func (s *Store) getOrCreateGroupByOIDCClaim(ctx context.Context, claimValue string) (uuid.UUID, error) {
	return s.getOrCreateGroupByLink(ctx, "oidc_group_claim", claimValue, claimValue, "oidc-sync")
}

// getOrCreateGroupByLink resolves the group linked to value via idColumn
// ("ldap_group_dn" or "oidc_group_claim" — always one of these two fixed
// literals, supplied only by the two wrappers above, never request input),
// auto-creating one — named displayName, with every permission flag false
// and no per-map grants — the first time a login references a directory/IdP
// group this server hasn't seen before. This mirrors CreateLDAPUser/
// CreateOIDCUser's "least privilege; an admin grants whatever access is
// appropriate afterward" precedent: the group exists and its members are
// tracked, but it grants nothing until an admin opts it into some
// capability via PUT /groups/{id}. actor (e.g. "ldap-sync"/"oidc-sync") is
// recorded in created_by/updated_by so an admin can tell an auto-created
// group apart from one they made themselves — not a real username, same as
// audit_logs.actor isn't constrained to be one.
func (s *Store) getOrCreateGroupByLink(ctx context.Context, idColumn, value, displayName, actor string) (uuid.UUID, error) {
	id, err := s.findGroupByLink(ctx, idColumn, value)
	if err == nil {
		return id, nil
	}

	if !errors.Is(err, ErrGroupNotFound) {
		return uuid.UUID{}, err
	}

	id, err = s.insertSyncedGroup(ctx, idColumn, value, displayName, actor)
	if err == nil {
		return id, nil
	}

	if !isPgErrCode(err, "23505") {
		return uuid.UUID{}, fmt.Errorf("create group: %w", err)
	}

	// Unique violation on the insert above is ambiguous: either a
	// concurrent sync just linked idColumn to value first (recheck and use
	// it), or displayName collides with some other, differently-linked
	// group's existing name (retry once with a random suffix, same
	// disambiguation CreateOIDCUser/CreateLDAPUser use for a colliding
	// username).
	if id, err := s.findGroupByLink(ctx, idColumn, value); err == nil {
		return id, nil
	}

	suffix, err := randomUsernameSuffix()
	if err != nil {
		return uuid.UUID{}, err
	}

	id, err = s.insertSyncedGroup(ctx, idColumn, value, displayName+"-"+suffix, actor)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("create group: %w", err)
	}

	return id, nil
}

// ldapGroupDisplayName picks the name an auto-created group gets for an LDAP
// entry dn: the value of its leading "cn=" component (e.g. "editors" for
// "cn=editors,ou=groups,dc=example,dc=com"), falling back to the full dn if
// it isn't shaped like "cn=..." — a display convenience only: dn itself, not
// this derived name, is what SyncGroupMembershipByLDAPDNs matches against
// (see ldap_group_dn).
func ldapGroupDisplayName(dn string) string {
	first, _, _ := strings.Cut(dn, ",")

	attr, name, ok := strings.Cut(first, "=")
	if !ok || !strings.EqualFold(strings.TrimSpace(attr), "cn") {
		return dn
	}

	return strings.TrimSpace(name)
}

// findGroupByLink returns the id of the group linked to value via idColumn,
// or ErrGroupNotFound if none is.
func (s *Store) findGroupByLink(ctx context.Context, idColumn, value string) (uuid.UUID, error) {
	var id uuid.UUID

	err := s.db.WithContext(ctx).Raw(`SELECT id FROM groups WHERE `+idColumn+` = ?`, value).Row().Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
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

	g := GroupRecord{
		ID:        id,
		Name:      name,
		CreatedBy: actor,
		UpdatedBy: actor,
	}

	if idColumn == "ldap_group_dn" {
		g.LDAPGroupDN = value
	} else {
		g.OIDCGroupClaim = value
	}

	if err := s.db.WithContext(ctx).Create(&g).Error; err != nil {
		return uuid.UUID{}, err
	}

	return id, nil
}
