package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	// ErrUserNotFound is returned when a user lookup finds no matching row.
	ErrUserNotFound = errors.New("user not found")
	// ErrUserExists is returned when creating a user whose username is already taken.
	ErrUserExists = errors.New("user already exists")
	// ErrUserOwnsMaps is returned by DeleteUser when username still owns
	// one or more maps (maps.owner_id references users(id) with no cascade,
	// see the "migrate maps owner_id column" migration step) — ownership
	// must be transferred to someone else first via UpdateMapOwner.
	ErrUserOwnsMaps = errors.New("user owns one or more maps")
)

// SyncUsername is the fixed local account every map, map version, and alias
// mirrored by internal/sync is created/updated/owned by (see
// Store.EnsureSyncUser) — one shared account across every configured
// remote, rather than one per remote, so ownership of mirrored content
// stays stable even if a remote is later renamed or removed (which remote a
// given map, in particular, came from is already tracked separately via
// maps.sync_remote_id).
const SyncUsername = "sync"

// UserRecord is the persisted form of a user account.
type UserRecord struct {
	Username            string    `json:"username"`
	CanCreate           bool      `json:"canCreate"`
	CanEdit             bool      `json:"canEdit"`
	CanDelete           bool      `json:"canDelete"`
	CanEditGeoObjects   bool      `json:"canEditGeoObjects"`
	CanDeleteGeoObjects bool      `json:"canDeleteGeoObjects"`
	CanViewAll          bool      `json:"canViewAll"`
	IsAdmin             bool      `json:"isAdmin"`
	CreatedAt           time.Time `json:"createdAt"`
}

// UserFilter holds optional filters for ListUsers. A zero value matches
// every user.
type UserFilter struct {
	Search              string // substring match against username, case-insensitive
	IsAdmin             *bool
	CanCreate           *bool
	CanEdit             *bool
	CanDelete           *bool
	CanEditGeoObjects   *bool
	CanDeleteGeoObjects *bool
	CanViewAll          *bool
}

// clauses returns the "column = $N"-style fragments for the filters set on
// f, binding their values through qb. Pure and DB-free so it's directly
// unit-testable.
func (f UserFilter) clauses(qb *queryBuilder) []string {
	var clauses []string

	if f.Search != "" {
		clauses = append(clauses, "username ILIKE "+qb.bind("%"+f.Search+"%"))
	}

	if f.IsAdmin != nil {
		clauses = append(clauses, "is_admin = "+qb.bind(*f.IsAdmin))
	}

	if f.CanCreate != nil {
		clauses = append(clauses, "can_create = "+qb.bind(*f.CanCreate))
	}

	if f.CanEdit != nil {
		clauses = append(clauses, "can_edit = "+qb.bind(*f.CanEdit))
	}

	if f.CanDelete != nil {
		clauses = append(clauses, "can_delete = "+qb.bind(*f.CanDelete))
	}

	if f.CanEditGeoObjects != nil {
		clauses = append(clauses, "can_edit_geo_objects = "+qb.bind(*f.CanEditGeoObjects))
	}

	if f.CanDeleteGeoObjects != nil {
		clauses = append(clauses, "can_delete_geo_objects = "+qb.bind(*f.CanDeleteGeoObjects))
	}

	if f.CanViewAll != nil {
		clauses = append(clauses, "can_view_all = "+qb.bind(*f.CanViewAll))
	}

	return clauses
}

// ListUsers returns every user matching filter, oldest first. filter's zero
// value matches everyone.
func (s *Store) ListUsers(ctx context.Context, filter UserFilter) ([]UserRecord, error) {
	qb := &queryBuilder{}

	where := ""
	if clauses := filter.clauses(qb); len(clauses) > 0 {
		where = "WHERE " + strings.Join(clauses, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT username, can_create, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, can_view_all, is_admin, created_at
		FROM users
		%s
		ORDER BY created_at ASC
	`, where)

	return collectRows(ctx, s.pool, "list users", query, func(rows pgx.Rows) (UserRecord, error) {
		var u UserRecord

		err := rows.Scan(&u.Username, &u.CanCreate, &u.CanEdit, &u.CanDelete, &u.CanEditGeoObjects, &u.CanDeleteGeoObjects, &u.CanViewAll, &u.IsAdmin, &u.CreatedAt)

		return u, err
	}, qb.args...)
}

// CreateUser creates a new user. It returns ErrUserExists if username is
// already taken.
func (s *Store) CreateUser(ctx context.Context, username, password string, perms Permissions) (UserRecord, error) {
	hash, err := hashPassword(password)
	if err != nil {
		return UserRecord{}, err
	}

	u := UserRecord{
		Username:            username,
		CanCreate:           perms.CanCreate,
		CanEdit:             perms.CanEdit,
		CanDelete:           perms.CanDelete,
		CanEditGeoObjects:   perms.CanEditGeoObjects,
		CanDeleteGeoObjects: perms.CanDeleteGeoObjects,
		CanViewAll:          perms.CanViewAll,
		IsAdmin:             perms.IsAdmin,
	}

	err = s.pool.QueryRow(ctx, `
		INSERT INTO users (username, password_hash, can_create, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, can_view_all, is_admin)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING created_at
	`, username, hash, u.CanCreate, u.CanEdit, u.CanDelete, u.CanEditGeoObjects, u.CanDeleteGeoObjects, u.CanViewAll, u.IsAdmin).Scan(&u.CreatedAt)
	if err != nil {
		if isPgErrCode(err, "23505") {
			return UserRecord{}, ErrUserExists
		}

		return UserRecord{}, fmt.Errorf("create user: %w", err)
	}

	return u, nil
}

// UpdateUser sets username's permissions, and its password too if
// newPassword is non-empty. It returns ErrUserNotFound if username doesn't
// exist.
func (s *Store) UpdateUser(ctx context.Context, username string, perms Permissions, newPassword string) (UserRecord, error) {
	var (
		u   UserRecord
		err error
	)

	if newPassword != "" {
		var hash string

		hash, err = hashPassword(newPassword)
		if err != nil {
			return UserRecord{}, err
		}

		err = s.pool.QueryRow(ctx, `
			UPDATE users
			SET can_create = $2, can_edit = $3, can_delete = $4, can_edit_geo_objects = $5, can_delete_geo_objects = $6, can_view_all = $7, is_admin = $8, password_hash = $9
			WHERE username = $1
			RETURNING username, can_create, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, can_view_all, is_admin, created_at
		`, username, perms.CanCreate, perms.CanEdit, perms.CanDelete, perms.CanEditGeoObjects, perms.CanDeleteGeoObjects, perms.CanViewAll, perms.IsAdmin, hash).
			Scan(&u.Username, &u.CanCreate, &u.CanEdit, &u.CanDelete, &u.CanEditGeoObjects, &u.CanDeleteGeoObjects, &u.CanViewAll, &u.IsAdmin, &u.CreatedAt)
	} else {
		err = s.pool.QueryRow(ctx, `
			UPDATE users
			SET can_create = $2, can_edit = $3, can_delete = $4, can_edit_geo_objects = $5, can_delete_geo_objects = $6, can_view_all = $7, is_admin = $8
			WHERE username = $1
			RETURNING username, can_create, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, can_view_all, is_admin, created_at
		`, username, perms.CanCreate, perms.CanEdit, perms.CanDelete, perms.CanEditGeoObjects, perms.CanDeleteGeoObjects, perms.CanViewAll, perms.IsAdmin).
			Scan(&u.Username, &u.CanCreate, &u.CanEdit, &u.CanDelete, &u.CanEditGeoObjects, &u.CanDeleteGeoObjects, &u.CanViewAll, &u.IsAdmin, &u.CreatedAt)
	}

	if errors.Is(err, pgx.ErrNoRows) {
		return UserRecord{}, ErrUserNotFound
	}

	if err != nil {
		return UserRecord{}, fmt.Errorf("update user: %w", err)
	}

	s.permsCache.invalidate(username)

	return u, nil
}

// EnsureSyncUser creates the fixed SyncUsername ("sync") account if it
// doesn't already exist yet, for internal/sync to attribute (as owner,
// created_by, and updated_by) every map/version/alias it mirrors from a
// remote. Idempotent and safe to call repeatedly — a no-op once the row
// exists, so callers (see sync.Manager's reconcile loop) can just call it
// unconditionally whenever sync is in use rather than tracking whether
// they've done so before.
//
// Its password_hash is set to a random value never handed back to anyone,
// the same approach CreateLDAPUser uses: with no usable local password, and
// (having no oidc_subject/ldap_dn either) no identity provider it's linked
// to, the account can never sign in through any login path. It's created
// with no global permissions (can_create/edit/delete and is_admin all
// false) — nothing it does goes through a permission check, since
// internal/sync calls Store methods directly rather than through the
// authenticated HTTP API.
func (s *Store) EnsureSyncUser(ctx context.Context) error {
	hash := ""

	const noPermissions = false

	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (username, password_hash, can_create, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, is_admin)
		VALUES ($1, $2, $3, $3, $3, $3, $3, $3)
		ON CONFLICT (username) DO NOTHING
	`, SyncUsername, hash, noPermissions)
	if err != nil {
		return fmt.Errorf("ensure sync user: %w", err)
	}

	return nil
}

// DeleteUser deletes username. It returns ErrUserNotFound if it doesn't
// exist, or ErrUserOwnsMaps if username still owns one or more maps.
func (s *Store) DeleteUser(ctx context.Context, username string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM users WHERE username = $1`, username)
	if err != nil {
		if isPgErrCode(err, "23503") {
			return ErrUserOwnsMaps
		}

		return fmt.Errorf("delete user: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	s.permsCache.invalidate(username)

	return nil
}
