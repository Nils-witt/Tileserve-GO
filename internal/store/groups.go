package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	// ErrGroupNotFound is returned when a group lookup finds no matching row.
	ErrGroupNotFound = errors.New("group not found")
	// ErrGroupExists is returned when creating a group whose name is already taken.
	ErrGroupExists = errors.New("group already exists")
)

// GroupRecord is the persisted form of a group. Its permission fields carry
// the same meaning as the matching fields on UserRecord/Permissions: every
// member inherits them, OR'd with their own personal flags (see
// Store.GetPermissions) and, for per-map grants, alongside their own
// map_permissions row (see Store.GetMapPermission). Membership itself is
// never stored or set here — it's derived from LDAPGroupDN/OIDCGroupClaim on
// every login (see group_membership.go).
type GroupRecord struct {
	ID                  uuid.UUID `json:"id"`
	Name                string    `json:"name"`
	CanCreate           bool      `json:"canCreate"`
	CanEdit             bool      `json:"canEdit"`
	CanDelete           bool      `json:"canDelete"`
	CanEditGeoObjects   bool      `json:"canEditGeoObjects"`
	CanDeleteGeoObjects bool      `json:"canDeleteGeoObjects"`
	CanViewAll          bool      `json:"canViewAll"`
	IsAdmin             bool      `json:"isAdmin"`
	LDAPGroupDN         string    `json:"ldapGroupDn"`
	OIDCGroupClaim      string    `json:"oidcGroupClaim"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
	CreatedBy           string    `json:"createdBy"`
	UpdatedBy           string    `json:"updatedBy"`
}

// GroupFilter holds optional filters for ListGroups. A zero value matches
// every group.
type GroupFilter struct {
	Search string // substring match against name, case-insensitive
}

// clauses returns the "column = $N"-style fragments for the filters set on
// f, binding their values through qb. Pure and DB-free so it's directly
// unit-testable.
func (f GroupFilter) clauses(qb *queryBuilder) []string {
	var clauses []string

	if f.Search != "" {
		clauses = append(clauses, "name ILIKE "+qb.bind("%"+f.Search+"%"))
	}

	return clauses
}

const groupSelectColumns = `
	id, name, can_create, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, can_view_all, is_admin,
	ldap_group_dn, oidc_group_claim, created_at, updated_at, created_by, updated_by
`

func scanGroup(row interface{ Scan(...any) error }, g *GroupRecord) error {
	return row.Scan(
		&g.ID, &g.Name, &g.CanCreate, &g.CanEdit, &g.CanDelete, &g.CanEditGeoObjects, &g.CanDeleteGeoObjects, &g.CanViewAll, &g.IsAdmin,
		&g.LDAPGroupDN, &g.OIDCGroupClaim, &g.CreatedAt, &g.UpdatedAt, &g.CreatedBy, &g.UpdatedBy,
	)
}

// ListGroups returns every group matching filter, oldest first. filter's
// zero value matches everyone.
func (s *Store) ListGroups(ctx context.Context, filter GroupFilter) ([]GroupRecord, error) {
	qb := &queryBuilder{}

	where := ""
	if clauses := filter.clauses(qb); len(clauses) > 0 {
		where = "WHERE " + strings.Join(clauses, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM groups
		%s
		ORDER BY created_at ASC
	`, groupSelectColumns, where)

	return collectRows(ctx, s.pool, "list groups", query, func(rows pgx.Rows) (GroupRecord, error) {
		var g GroupRecord

		err := scanGroup(rows, &g)

		return g, err
	}, qb.args...)
}

// GetGroup returns the group with the given id. It returns ErrGroupNotFound
// if no such group exists.
func (s *Store) GetGroup(ctx context.Context, id uuid.UUID) (GroupRecord, error) {
	var g GroupRecord

	err := scanGroup(s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s FROM groups WHERE id = $1
	`, groupSelectColumns), id), &g)
	if errors.Is(err, pgx.ErrNoRows) {
		return GroupRecord{}, ErrGroupNotFound
	}

	if err != nil {
		return GroupRecord{}, fmt.Errorf("get group: %w", err)
	}

	return g, nil
}

// CreateGroup creates a new group. It returns ErrGroupExists if name is
// already taken.
func (s *Store) CreateGroup(ctx context.Context, name string, perms Permissions, ldapGroupDN, oidcGroupClaim, actor string) (GroupRecord, error) {
	g := GroupRecord{
		ID:                  uuid.New(),
		Name:                name,
		CanCreate:           perms.CanCreate,
		CanEdit:             perms.CanEdit,
		CanDelete:           perms.CanDelete,
		CanEditGeoObjects:   perms.CanEditGeoObjects,
		CanDeleteGeoObjects: perms.CanDeleteGeoObjects,
		CanViewAll:          perms.CanViewAll,
		IsAdmin:             perms.IsAdmin,
		LDAPGroupDN:         ldapGroupDN,
		OIDCGroupClaim:      oidcGroupClaim,
		CreatedBy:           actor,
		UpdatedBy:           actor,
	}

	err := s.pool.QueryRow(ctx, `
		INSERT INTO groups (id, name, can_create, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, can_view_all, is_admin, ldap_group_dn, oidc_group_claim, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12)
		RETURNING created_at, updated_at
	`, g.ID, g.Name, g.CanCreate, g.CanEdit, g.CanDelete, g.CanEditGeoObjects, g.CanDeleteGeoObjects, g.CanViewAll, g.IsAdmin, g.LDAPGroupDN, g.OIDCGroupClaim, actor).
		Scan(&g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if isPgErrCode(err, "23505") {
			return GroupRecord{}, ErrGroupExists
		}

		return GroupRecord{}, fmt.Errorf("create group: %w", err)
	}

	return g, nil
}

// UpdateGroup replaces id's name, permission bundle, and LDAP/OIDC linking
// fields. It returns ErrGroupNotFound if id doesn't exist, or ErrGroupExists
// if name collides with a different group. Every cached permission and
// per-map-permission entry is cleared: a group's members aren't cheaply
// enumerable in reverse, so a targeted invalidation isn't possible here (see
// ttlCache.clear).
func (s *Store) UpdateGroup(ctx context.Context, id uuid.UUID, name string, perms Permissions, ldapGroupDN, oidcGroupClaim, actor string) (GroupRecord, error) {
	var g GroupRecord

	err := scanGroup(s.pool.QueryRow(ctx, fmt.Sprintf(`
		UPDATE groups
		SET name = $2, can_create = $3, can_edit = $4, can_delete = $5, can_edit_geo_objects = $6, can_delete_geo_objects = $7, can_view_all = $8, is_admin = $9,
			ldap_group_dn = $10, oidc_group_claim = $11, updated_by = $12, updated_at = now()
		WHERE id = $1
		RETURNING %s
	`, groupSelectColumns), id, name, perms.CanCreate, perms.CanEdit, perms.CanDelete, perms.CanEditGeoObjects, perms.CanDeleteGeoObjects, perms.CanViewAll, perms.IsAdmin, ldapGroupDN, oidcGroupClaim, actor), &g)
	if errors.Is(err, pgx.ErrNoRows) {
		return GroupRecord{}, ErrGroupNotFound
	}

	if err != nil {
		if isPgErrCode(err, "23505") {
			return GroupRecord{}, ErrGroupExists
		}

		return GroupRecord{}, fmt.Errorf("update group: %w", err)
	}

	s.permsCache.clear()
	s.mapPermCache.clear()

	return g, nil
}

// DeleteGroup deletes the group with the given id. It returns
// ErrGroupNotFound if it doesn't exist. group_members/group_map_permissions
// rows for it are removed via ON DELETE CASCADE.
func (s *Store) DeleteGroup(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM groups WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete group: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrGroupNotFound
	}

	s.permsCache.clear()
	s.mapPermCache.clear()

	return nil
}
