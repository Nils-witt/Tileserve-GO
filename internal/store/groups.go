package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
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
	ID                  uuid.UUID `json:"id" gorm:"column:id;type:uuid;primaryKey"`
	Name                string    `json:"name" gorm:"column:name;not null;uniqueIndex"`
	CanCreate           bool      `json:"canCreate" gorm:"column:can_create;not null;default:false"`
	CanEdit             bool      `json:"canEdit" gorm:"column:can_edit;not null;default:false"`
	CanDelete           bool      `json:"canDelete" gorm:"column:can_delete;not null;default:false"`
	CanEditGeoObjects   bool      `json:"canEditGeoObjects" gorm:"column:can_edit_geo_objects;not null;default:false"`
	CanDeleteGeoObjects bool      `json:"canDeleteGeoObjects" gorm:"column:can_delete_geo_objects;not null;default:false"`
	CanViewAll          bool      `json:"canViewAll" gorm:"column:can_view_all;not null;default:false"`
	IsAdmin             bool      `json:"isAdmin" gorm:"column:is_admin;not null;default:false"`
	LDAPGroupDN         string    `json:"ldapGroupDn" gorm:"column:ldap_group_dn;not null;default:''"`
	OIDCGroupClaim      string    `json:"oidcGroupClaim" gorm:"column:oidc_group_claim;not null;default:''"`
	CreatedAt           time.Time `json:"createdAt" gorm:"column:created_at;not null;default:now()"`
	UpdatedAt           time.Time `json:"updatedAt" gorm:"column:updated_at;not null;default:now()"`
	CreatedBy           string    `json:"createdBy" gorm:"column:created_by;not null"`
	UpdatedBy           string    `json:"updatedBy" gorm:"column:updated_by;not null"`
}

// TableName implements the gorm.Tabler interface.
func (GroupRecord) TableName() string { return "groups" }

// GroupFilter holds optional filters for ListGroups. A zero value matches
// every group.
type GroupFilter struct {
	Search string // substring match against name, case-insensitive
}

// Scope applies f's set filters to db as additional WHERE conditions.
func (f GroupFilter) Scope() func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if f.Search != "" {
			db = db.Where("name ILIKE ?", "%"+f.Search+"%")
		}

		return db
	}
}

// ListGroups returns every group matching filter, oldest first. filter's
// zero value matches everyone.
func (s *Store) ListGroups(ctx context.Context, filter GroupFilter) ([]GroupRecord, error) {
	groups := []GroupRecord{}

	err := s.db.WithContext(ctx).Scopes(filter.Scope()).Order("created_at ASC").Find(&groups).Error
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}

	return groups, nil
}

// GetGroup returns the group with the given id. It returns ErrGroupNotFound
// if no such group exists.
func (s *Store) GetGroup(ctx context.Context, id uuid.UUID) (GroupRecord, error) {
	var g GroupRecord

	err := s.db.WithContext(ctx).Where("id = ?", id).Take(&g).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
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

	if err := s.db.WithContext(ctx).Create(&g).Error; err != nil {
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
	res := s.db.WithContext(ctx).Model(&GroupRecord{}).Where("id = ?", id).Updates(map[string]any{
		colName:                  name,
		"can_create":             perms.CanCreate,
		"can_edit":               perms.CanEdit,
		"can_delete":             perms.CanDelete,
		"can_edit_geo_objects":   perms.CanEditGeoObjects,
		"can_delete_geo_objects": perms.CanDeleteGeoObjects,
		"can_view_all":           perms.CanViewAll,
		"is_admin":               perms.IsAdmin,
		"ldap_group_dn":          ldapGroupDN,
		"oidc_group_claim":       oidcGroupClaim,
		colUpdatedBy:             actor,
		colUpdatedAt:             gorm.Expr("now()"),
	})
	if res.Error != nil {
		if isPgErrCode(res.Error, "23505") {
			return GroupRecord{}, ErrGroupExists
		}

		return GroupRecord{}, fmt.Errorf("update group: %w", res.Error)
	}

	if res.RowsAffected == 0 {
		return GroupRecord{}, ErrGroupNotFound
	}

	var g GroupRecord
	if err := s.db.WithContext(ctx).Where("id = ?", id).Take(&g).Error; err != nil {
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
	res := s.db.WithContext(ctx).Where("id = ?", id).Delete(&GroupRecord{})
	if res.Error != nil {
		return fmt.Errorf("delete group: %w", res.Error)
	}

	if res.RowsAffected == 0 {
		return ErrGroupNotFound
	}

	s.permsCache.clear()
	s.mapPermCache.clear()

	return nil
}
