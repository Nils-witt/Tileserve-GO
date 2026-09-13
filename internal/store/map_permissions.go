package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrMapPermissionInvalid is returned when granting a permission references a map or username that does not exist.
var ErrMapPermissionInvalid = errors.New("map or username does not exist")

// mapPermissionModel is the GORM-mapped form of one row in map_permissions;
// MapPermissionRecord (the public type) omits MapUUID since every caller
// already knows it from context.
type mapPermissionModel struct {
	MapUUID             uuid.UUID `gorm:"column:map_uuid;type:uuid;primaryKey"`
	Username            string    `gorm:"column:username;primaryKey"`
	CanView             bool      `gorm:"column:can_view;not null;default:false"`
	CanEdit             bool      `gorm:"column:can_edit;not null;default:false"`
	CanDelete           bool      `gorm:"column:can_delete;not null;default:false"`
	CanEditGeoObjects   bool      `gorm:"column:can_edit_geo_objects;not null;default:false"`
	CanDeleteGeoObjects bool      `gorm:"column:can_delete_geo_objects;not null;default:false"`
	GrantedAt           time.Time `gorm:"column:granted_at;not null;default:now()"`
	GrantedBy           string    `gorm:"column:granted_by;not null"`
}

func (mapPermissionModel) TableName() string { return "map_permissions" }

// MapPermission is a user's per-map view/edit/delete grant. It only adds
// capability on top of a user's global Permissions (see Permissions in
// store.go); a grant is only consulted for a user who lacks the matching
// global flag, so it can never take capability away. Edit and delete grants
// also imply view, since granting someone the ability to modify a map
// without letting them see it first would be nonsensical. CanEditGeoObjects
// and CanDeleteGeoObjects are separate from CanEdit/CanDelete: they grant
// (and imply view via) geo object write access specifically, without also
// granting the ability to edit/delete the map itself, its versions, or its
// aliases.
type MapPermission struct {
	CanView             bool
	CanEdit             bool
	CanDelete           bool
	CanEditGeoObjects   bool
	CanDeleteGeoObjects bool
}

// GrantsVisibility reports whether mp, on its own, is enough to make its
// map visible to its holder: any of the five grants implies view.
func (mp MapPermission) GrantsVisibility() bool {
	return mp.CanView || mp.CanEdit || mp.CanDelete || mp.CanEditGeoObjects || mp.CanDeleteGeoObjects
}

// MapPermissionRecord is the persisted form of a per-map permission grant.
type MapPermissionRecord struct {
	Username            string    `json:"username"`
	CanView             bool      `json:"canView"`
	CanEdit             bool      `json:"canEdit"`
	CanDelete           bool      `json:"canDelete"`
	CanEditGeoObjects   bool      `json:"canEditGeoObjects"`
	CanDeleteGeoObjects bool      `json:"canDeleteGeoObjects"`
	GrantedAt           time.Time `json:"grantedAt"`
	GrantedBy           string    `json:"grantedBy"`
}

// GetMapPermission returns username's per-map grant for mapID, OR'd together
// with the per-map grant of every group username currently belongs to (see
// group_members/group_map_permissions), or the zero value (no
// view/edit/delete from any source) if none exists. Results are cached for
// cacheTTL, since this is on the tile-serving/map-view hot path.
func (s *Store) GetMapPermission(ctx context.Context, mapID uuid.UUID, username string) (MapPermission, error) {
	key := mapPermKey{mapID: mapID, username: username}
	if mp, ok := s.mapPermCache.get(key); ok {
		return mp, nil
	}

	// Unlike GetPermissions, "no grant from any source" is the common case
	// here, and the UNION ALL below always returns exactly one row (an
	// aggregate with no GROUP BY never returns zero rows) — so, unlike a
	// plain SELECT, there's no not-found case, only an all-NULL row when
	// neither source has a matching grant. Each column scans into
	// sql.NullBool rather than plain bool for that reason; NullBool.Bool is
	// already false when Valid is false, which correctly collapses to the
	// same zero-value MapPermission{} a not-found row would give.
	var canView, canEdit, canDelete, canEditGeo, canDeleteGeo sql.NullBool

	err := s.db.WithContext(ctx).Raw(`
		SELECT
			bool_or(can_view), bool_or(can_edit), bool_or(can_delete),
			bool_or(can_edit_geo_objects), bool_or(can_delete_geo_objects)
		FROM (
			SELECT can_view, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects
			FROM map_permissions WHERE map_uuid = ? AND username = ?
			UNION ALL
			SELECT gmp.can_view, gmp.can_edit, gmp.can_delete, gmp.can_edit_geo_objects, gmp.can_delete_geo_objects
			FROM group_map_permissions gmp
			JOIN group_members gm ON gm.group_id = gmp.group_id
			WHERE gmp.map_uuid = ? AND gm.username = ?
		) combined
	`, mapID, username, mapID, username).Row().Scan(&canView, &canEdit, &canDelete, &canEditGeo, &canDeleteGeo)
	if err != nil {
		return MapPermission{}, fmt.Errorf("get map permission: %w", err)
	}

	mp := MapPermission{
		CanView:             canView.Bool,
		CanEdit:             canEdit.Bool,
		CanDelete:           canDelete.Bool,
		CanEditGeoObjects:   canEditGeo.Bool,
		CanDeleteGeoObjects: canDeleteGeo.Bool,
	}

	s.mapPermCache.set(key, mp)

	return mp, nil
}

// MapPermissionEntry pairs a per-map permission grant with the map it
// applies to, letting an admin audit every per-map grant across every map
// (see ListAllMapPermissions) without listing them one map at a time.
type MapPermissionEntry struct {
	Map        MapRecord           `json:"map"`
	Permission MapPermissionRecord `json:"permission"`
}

// ListAllMapPermissions returns every per-map permission grant across every
// map, oldest-granted first, each paired with the map it applies to.
func (s *Store) ListAllMapPermissions(ctx context.Context) ([]MapPermissionEntry, error) {
	rows, err := s.db.WithContext(ctx).Raw(`
		SELECT
			m.uuid, m.name, m.current_version, m.visible_to_all, m.anonymous_allowed, m.created_at, m.updated_at, m.created_by, m.updated_by, owner_user.username,
			mp.username, mp.can_view, mp.can_edit, mp.can_delete, mp.can_edit_geo_objects, mp.can_delete_geo_objects, mp.granted_at, mp.granted_by
		FROM map_permissions mp
		JOIN maps m ON m.uuid = mp.map_uuid
		JOIN users owner_user ON owner_user.id = m.owner_id
		ORDER BY mp.granted_at ASC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("list all map permissions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entries := []MapPermissionEntry{}

	for rows.Next() {
		var e MapPermissionEntry

		err := rows.Scan(
			&e.Map.UUID, &e.Map.Name, &e.Map.CurrentVersion, &e.Map.VisibleToAll, &e.Map.AnonymousAllowed, &e.Map.CreatedAt, &e.Map.UpdatedAt, &e.Map.CreatedBy, &e.Map.UpdatedBy, &e.Map.Owner,
			&e.Permission.Username, &e.Permission.CanView, &e.Permission.CanEdit, &e.Permission.CanDelete, &e.Permission.CanEditGeoObjects, &e.Permission.CanDeleteGeoObjects, &e.Permission.GrantedAt, &e.Permission.GrantedBy,
		)
		if err != nil {
			return nil, fmt.Errorf("list all map permissions: %w", err)
		}

		entries = append(entries, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list all map permissions: %w", err)
	}

	return entries, nil
}

// ListMapPermissions returns every per-map grant for mapID, oldest first.
func (s *Store) ListMapPermissions(ctx context.Context, mapID uuid.UUID) ([]MapPermissionRecord, error) {
	perms := []MapPermissionRecord{}

	err := s.db.WithContext(ctx).Table("map_permissions").
		Where("map_uuid = ?", mapID).
		Order("granted_at ASC").
		Find(&perms).Error
	if err != nil {
		return nil, fmt.Errorf("list map permissions: %w", err)
	}

	return perms, nil
}

// SetMapPermission creates or replaces username's per-map grant for mapID.
// It returns ErrMapPermissionInvalid if mapID or username don't exist.
func (s *Store) SetMapPermission(ctx context.Context, mapID uuid.UUID, username string, canView, canEdit, canDelete, canEditGeoObjects, canDeleteGeoObjects bool, grantedBy string) (MapPermissionRecord, error) {
	var grantedAt time.Time

	err := s.db.WithContext(ctx).Raw(`
		INSERT INTO map_permissions (map_uuid, username, can_view, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, granted_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (map_uuid, username)
		DO UPDATE SET can_view = ?, can_edit = ?, can_delete = ?, can_edit_geo_objects = ?, can_delete_geo_objects = ?, granted_by = ?, granted_at = now()
		RETURNING granted_at
	`, mapID, username, canView, canEdit, canDelete, canEditGeoObjects, canDeleteGeoObjects, grantedBy,
		canView, canEdit, canDelete, canEditGeoObjects, canDeleteGeoObjects, grantedBy).
		Row().Scan(&grantedAt)
	if err != nil {
		if isPgErrCode(err, "23503") {
			return MapPermissionRecord{}, ErrMapPermissionInvalid
		}

		return MapPermissionRecord{}, fmt.Errorf("set map permission: %w", err)
	}

	s.mapPermCache.invalidate(mapPermKey{mapID: mapID, username: username})

	return MapPermissionRecord{
		Username:            username,
		CanView:             canView,
		CanEdit:             canEdit,
		CanDelete:           canDelete,
		CanEditGeoObjects:   canEditGeoObjects,
		CanDeleteGeoObjects: canDeleteGeoObjects,
		GrantedAt:           grantedAt,
		GrantedBy:           grantedBy,
	}, nil
}

// DeleteMapPermission revokes username's per-map grant for mapID, if any.
func (s *Store) DeleteMapPermission(ctx context.Context, mapID uuid.UUID, username string) error {
	err := s.db.WithContext(ctx).
		Where("map_uuid = ? AND username = ?", mapID, username).
		Delete(&mapPermissionModel{}).Error
	if err != nil {
		return fmt.Errorf("delete map permission: %w", err)
	}

	s.mapPermCache.invalidate(mapPermKey{mapID: mapID, username: username})

	return nil
}
