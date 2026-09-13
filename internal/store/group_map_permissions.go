package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// groupMapPermissionModel is the GORM-mapped form of one row in
// group_map_permissions; GroupMapPermissionRecord (the public type) omits
// MapUUID since every caller already knows it from context.
type groupMapPermissionModel struct {
	MapUUID             uuid.UUID `gorm:"column:map_uuid;type:uuid;primaryKey"`
	GroupID             uuid.UUID `gorm:"column:group_id;type:uuid;primaryKey"`
	CanView             bool      `gorm:"column:can_view;not null;default:false"`
	CanEdit             bool      `gorm:"column:can_edit;not null;default:false"`
	CanDelete           bool      `gorm:"column:can_delete;not null;default:false"`
	CanEditGeoObjects   bool      `gorm:"column:can_edit_geo_objects;not null;default:false"`
	CanDeleteGeoObjects bool      `gorm:"column:can_delete_geo_objects;not null;default:false"`
	GrantedAt           time.Time `gorm:"column:granted_at;not null;default:now()"`
	GrantedBy           string    `gorm:"column:granted_by;not null"`
}

func (groupMapPermissionModel) TableName() string { return "group_map_permissions" }

// GroupMapPermissionRecord is the persisted form of a per-map permission
// grant made to a group — identical in shape to MapPermissionRecord, but
// every current member of the group inherits it (see
// Store.GetMapPermission's UNION ALL query).
type GroupMapPermissionRecord struct {
	GroupID             uuid.UUID `json:"groupId"`
	CanView             bool      `json:"canView"`
	CanEdit             bool      `json:"canEdit"`
	CanDelete           bool      `json:"canDelete"`
	CanEditGeoObjects   bool      `json:"canEditGeoObjects"`
	CanDeleteGeoObjects bool      `json:"canDeleteGeoObjects"`
	GrantedAt           time.Time `json:"grantedAt"`
	GrantedBy           string    `json:"grantedBy"`
}

// ListGroupMapPermissions returns every group grant for mapID, oldest first.
func (s *Store) ListGroupMapPermissions(ctx context.Context, mapID uuid.UUID) ([]GroupMapPermissionRecord, error) {
	perms := []GroupMapPermissionRecord{}

	err := s.db.WithContext(ctx).Table("group_map_permissions").
		Where("map_uuid = ?", mapID).
		Order("granted_at ASC").
		Find(&perms).Error
	if err != nil {
		return nil, fmt.Errorf("list group map permissions: %w", err)
	}

	return perms, nil
}

// SetGroupMapPermission creates or replaces groupID's per-map grant for
// mapID. It returns ErrMapPermissionInvalid if mapID or groupID don't exist.
// Every cached per-map permission entry is cleared (see
// Store.UpdateGroup for why a targeted invalidation isn't possible here).
func (s *Store) SetGroupMapPermission(ctx context.Context, mapID, groupID uuid.UUID, canView, canEdit, canDelete, canEditGeoObjects, canDeleteGeoObjects bool, grantedBy string) (GroupMapPermissionRecord, error) {
	var grantedAt time.Time

	err := s.db.WithContext(ctx).Raw(`
		INSERT INTO group_map_permissions (map_uuid, group_id, can_view, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, granted_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (map_uuid, group_id)
		DO UPDATE SET can_view = ?, can_edit = ?, can_delete = ?, can_edit_geo_objects = ?, can_delete_geo_objects = ?, granted_by = ?, granted_at = now()
		RETURNING granted_at
	`, mapID, groupID, canView, canEdit, canDelete, canEditGeoObjects, canDeleteGeoObjects, grantedBy,
		canView, canEdit, canDelete, canEditGeoObjects, canDeleteGeoObjects, grantedBy).
		Row().Scan(&grantedAt)
	if err != nil {
		if isPgErrCode(err, "23503") {
			return GroupMapPermissionRecord{}, ErrMapPermissionInvalid
		}

		return GroupMapPermissionRecord{}, fmt.Errorf("set group map permission: %w", err)
	}

	s.mapPermCache.clear()

	return GroupMapPermissionRecord{
		GroupID:             groupID,
		CanView:             canView,
		CanEdit:             canEdit,
		CanDelete:           canDelete,
		CanEditGeoObjects:   canEditGeoObjects,
		CanDeleteGeoObjects: canDeleteGeoObjects,
		GrantedAt:           grantedAt,
		GrantedBy:           grantedBy,
	}, nil
}

// DeleteGroupMapPermission revokes groupID's per-map grant for mapID, if any.
func (s *Store) DeleteGroupMapPermission(ctx context.Context, mapID, groupID uuid.UUID) error {
	err := s.db.WithContext(ctx).
		Where("map_uuid = ? AND group_id = ?", mapID, groupID).
		Delete(&groupMapPermissionModel{}).Error
	if err != nil {
		return fmt.Errorf("delete group map permission: %w", err)
	}

	s.mapPermCache.clear()

	return nil
}
