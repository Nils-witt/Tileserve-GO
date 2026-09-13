package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// ErrUserNotFound is returned when a user lookup finds no matching row.
	ErrUserNotFound = errors.New("user not found")
	// ErrUserExists is returned when creating a user whose username is already taken.
	ErrUserExists = errors.New("user already exists")
	// ErrUserOwnsMaps is returned by DeleteUser when username still owns
	// one or more maps (maps.owner_id references users(id) with no cascade,
	// see the fk_maps_owner constraint) — ownership must be transferred to
	// someone else first via UpdateMapOwner.
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
	// ID has no public accessor: every relationship in this schema keys off
	// Username, not this numeric id, except maps.owner_id — a real column
	// GORM needs a primary key to reference, but never surfaced to callers.
	ID                  int64     `json:"-" gorm:"column:id;primaryKey;autoIncrement"`
	Username            string    `json:"username" gorm:"column:username;not null;uniqueIndex"`
	PasswordHash        string    `json:"-" gorm:"column:password_hash;not null"`
	CanCreate           bool      `json:"canCreate" gorm:"column:can_create;not null"`
	CanEdit             bool      `json:"canEdit" gorm:"column:can_edit;not null"`
	CanDelete           bool      `json:"canDelete" gorm:"column:can_delete;not null"`
	CanEditGeoObjects   bool      `json:"canEditGeoObjects" gorm:"column:can_edit_geo_objects;not null"`
	CanDeleteGeoObjects bool      `json:"canDeleteGeoObjects" gorm:"column:can_delete_geo_objects;not null"`
	CanViewAll          bool      `json:"canViewAll" gorm:"column:can_view_all;not null;default:false"`
	IsAdmin             bool      `json:"isAdmin" gorm:"column:is_admin;not null"`
	OIDCIssuer          string    `json:"-" gorm:"column:oidc_issuer;not null;default:''"`
	OIDCSubject         string    `json:"-" gorm:"column:oidc_subject;not null;default:''"`
	LDAPDN              string    `json:"-" gorm:"column:ldap_dn;not null;default:''"`
	CreatedAt           time.Time `json:"createdAt" gorm:"column:created_at;not null;default:now()"`
}

// TableName implements the gorm.Tabler interface.
func (UserRecord) TableName() string { return "users" }

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

// Scope applies f's set filters to db as additional WHERE conditions.
func (f UserFilter) Scope() func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if f.Search != "" {
			db = db.Where("username ILIKE ?", "%"+f.Search+"%")
		}

		if f.IsAdmin != nil {
			db = db.Where("is_admin = ?", *f.IsAdmin)
		}

		if f.CanCreate != nil {
			db = db.Where("can_create = ?", *f.CanCreate)
		}

		if f.CanEdit != nil {
			db = db.Where("can_edit = ?", *f.CanEdit)
		}

		if f.CanDelete != nil {
			db = db.Where("can_delete = ?", *f.CanDelete)
		}

		if f.CanEditGeoObjects != nil {
			db = db.Where("can_edit_geo_objects = ?", *f.CanEditGeoObjects)
		}

		if f.CanDeleteGeoObjects != nil {
			db = db.Where("can_delete_geo_objects = ?", *f.CanDeleteGeoObjects)
		}

		if f.CanViewAll != nil {
			db = db.Where("can_view_all = ?", *f.CanViewAll)
		}

		return db
	}
}

// ListUsers returns every user matching filter, oldest first. filter's zero
// value matches everyone.
func (s *Store) ListUsers(ctx context.Context, filter UserFilter) ([]UserRecord, error) {
	users := []UserRecord{}

	err := s.db.WithContext(ctx).Scopes(filter.Scope()).Order("created_at ASC").Find(&users).Error
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}

	return users, nil
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
		PasswordHash:        hash,
		CanCreate:           perms.CanCreate,
		CanEdit:             perms.CanEdit,
		CanDelete:           perms.CanDelete,
		CanEditGeoObjects:   perms.CanEditGeoObjects,
		CanDeleteGeoObjects: perms.CanDeleteGeoObjects,
		CanViewAll:          perms.CanViewAll,
		IsAdmin:             perms.IsAdmin,
	}

	if err := s.db.WithContext(ctx).Create(&u).Error; err != nil {
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
	updates := map[string]any{
		"can_create":             perms.CanCreate,
		"can_edit":               perms.CanEdit,
		"can_delete":             perms.CanDelete,
		"can_edit_geo_objects":   perms.CanEditGeoObjects,
		"can_delete_geo_objects": perms.CanDeleteGeoObjects,
		"can_view_all":           perms.CanViewAll,
		"is_admin":               perms.IsAdmin,
	}

	if newPassword != "" {
		hash, err := hashPassword(newPassword)
		if err != nil {
			return UserRecord{}, err
		}

		updates["password_hash"] = hash
	}

	res := s.db.WithContext(ctx).Model(&UserRecord{}).Where("username = ?", username).Updates(updates)
	if res.Error != nil {
		return UserRecord{}, fmt.Errorf("update user: %w", res.Error)
	}

	if res.RowsAffected == 0 {
		return UserRecord{}, ErrUserNotFound
	}

	var u UserRecord
	if err := s.db.WithContext(ctx).Where("username = ?", username).Take(&u).Error; err != nil {
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
	u := UserRecord{Username: SyncUsername, PasswordHash: ""}

	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&u).Error
	if err != nil {
		return fmt.Errorf("ensure sync user: %w", err)
	}

	return nil
}

// DeleteUser deletes username. It returns ErrUserNotFound if it doesn't
// exist, or ErrUserOwnsMaps if username still owns one or more maps.
func (s *Store) DeleteUser(ctx context.Context, username string) error {
	res := s.db.WithContext(ctx).Where("username = ?", username).Delete(&UserRecord{})
	if res.Error != nil {
		if isPgErrCode(res.Error, "23503") {
			return ErrUserOwnsMaps
		}

		return fmt.Errorf("delete user: %w", res.Error)
	}

	if res.RowsAffected == 0 {
		return ErrUserNotFound
	}

	s.permsCache.invalidate(username)

	return nil
}
