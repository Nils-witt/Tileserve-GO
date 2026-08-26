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
)

// UserRecord is the persisted form of a user account.
type UserRecord struct {
	Username            string    `json:"username"`
	CanCreate           bool      `json:"canCreate"`
	CanEdit             bool      `json:"canEdit"`
	CanDelete           bool      `json:"canDelete"`
	CanEditGeoObjects   bool      `json:"canEditGeoObjects"`
	CanDeleteGeoObjects bool      `json:"canDeleteGeoObjects"`
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
		SELECT username, can_create, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, is_admin, created_at
		FROM users
		%s
		ORDER BY created_at ASC
	`, where)

	return collectRows(ctx, s.pool, "list users", query, func(rows pgx.Rows) (UserRecord, error) {
		var u UserRecord

		err := rows.Scan(&u.Username, &u.CanCreate, &u.CanEdit, &u.CanDelete, &u.CanEditGeoObjects, &u.CanDeleteGeoObjects, &u.IsAdmin, &u.CreatedAt)

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
		IsAdmin:             perms.IsAdmin,
	}

	err = s.pool.QueryRow(ctx, `
		INSERT INTO users (username, password_hash, can_create, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, is_admin)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at
	`, username, hash, u.CanCreate, u.CanEdit, u.CanDelete, u.CanEditGeoObjects, u.CanDeleteGeoObjects, u.IsAdmin).Scan(&u.CreatedAt)
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
			SET can_create = $2, can_edit = $3, can_delete = $4, can_edit_geo_objects = $5, can_delete_geo_objects = $6, is_admin = $7, password_hash = $8
			WHERE username = $1
			RETURNING username, can_create, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, is_admin, created_at
		`, username, perms.CanCreate, perms.CanEdit, perms.CanDelete, perms.CanEditGeoObjects, perms.CanDeleteGeoObjects, perms.IsAdmin, hash).
			Scan(&u.Username, &u.CanCreate, &u.CanEdit, &u.CanDelete, &u.CanEditGeoObjects, &u.CanDeleteGeoObjects, &u.IsAdmin, &u.CreatedAt)
	} else {
		err = s.pool.QueryRow(ctx, `
			UPDATE users
			SET can_create = $2, can_edit = $3, can_delete = $4, can_edit_geo_objects = $5, can_delete_geo_objects = $6, is_admin = $7
			WHERE username = $1
			RETURNING username, can_create, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, is_admin, created_at
		`, username, perms.CanCreate, perms.CanEdit, perms.CanDelete, perms.CanEditGeoObjects, perms.CanDeleteGeoObjects, perms.IsAdmin).
			Scan(&u.Username, &u.CanCreate, &u.CanEdit, &u.CanDelete, &u.CanEditGeoObjects, &u.CanDeleteGeoObjects, &u.IsAdmin, &u.CreatedAt)
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

// DeleteUser deletes username. It returns ErrUserNotFound if it doesn't exist.
func (s *Store) DeleteUser(ctx context.Context, username string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM users WHERE username = $1`, username)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	s.permsCache.invalidate(username)

	return nil
}
