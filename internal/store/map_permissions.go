package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrMapPermissionInvalid is returned when granting a permission references a map or username that does not exist.
var ErrMapPermissionInvalid = errors.New("map or username does not exist")

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

// GetMapPermission returns username's per-map grant for mapID, or the zero
// value (no view/edit/delete) if none exists. Results are cached for
// cacheTTL, since this is on the tile-serving/map-view hot path.
func (s *Store) GetMapPermission(ctx context.Context, mapID uuid.UUID, username string) (MapPermission, error) {
	key := mapPermKey{mapID: mapID, username: username}
	if mp, ok := s.mapPermCache.get(key); ok {
		return mp, nil
	}

	var mp MapPermission

	err := s.pool.QueryRow(ctx, `
		SELECT can_view, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects FROM map_permissions WHERE map_uuid = $1 AND username = $2
	`, mapID, username).Scan(&mp.CanView, &mp.CanEdit, &mp.CanDelete, &mp.CanEditGeoObjects, &mp.CanDeleteGeoObjects)
	if errors.Is(err, pgx.ErrNoRows) {
		s.mapPermCache.set(key, MapPermission{})
		return MapPermission{}, nil
	}

	if err != nil {
		return MapPermission{}, fmt.Errorf("get map permission: %w", err)
	}

	s.mapPermCache.set(key, mp)

	return mp, nil
}

// ListMapPermissions returns every per-map grant for mapID, oldest first.
func (s *Store) ListMapPermissions(ctx context.Context, mapID uuid.UUID) ([]MapPermissionRecord, error) {
	return collectRows(ctx, s.pool, "list map permissions", `
		SELECT username, can_view, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, granted_at, granted_by
		FROM map_permissions
		WHERE map_uuid = $1
		ORDER BY granted_at ASC
	`, func(rows pgx.Rows) (MapPermissionRecord, error) {
		var p MapPermissionRecord

		err := rows.Scan(&p.Username, &p.CanView, &p.CanEdit, &p.CanDelete, &p.CanEditGeoObjects, &p.CanDeleteGeoObjects, &p.GrantedAt, &p.GrantedBy)

		return p, err
	}, mapID)
}

// SetMapPermission creates or replaces username's per-map grant for mapID.
// It returns ErrMapPermissionInvalid if mapID or username don't exist.
func (s *Store) SetMapPermission(ctx context.Context, mapID uuid.UUID, username string, canView, canEdit, canDelete, canEditGeoObjects, canDeleteGeoObjects bool, grantedBy string) (MapPermissionRecord, error) {
	p := MapPermissionRecord{
		Username:            username,
		CanView:             canView,
		CanEdit:             canEdit,
		CanDelete:           canDelete,
		CanEditGeoObjects:   canEditGeoObjects,
		CanDeleteGeoObjects: canDeleteGeoObjects,
		GrantedBy:           grantedBy,
	}

	err := s.pool.QueryRow(ctx, `
		INSERT INTO map_permissions (map_uuid, username, can_view, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, granted_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (map_uuid, username)
		DO UPDATE SET can_view = $3, can_edit = $4, can_delete = $5, can_edit_geo_objects = $6, can_delete_geo_objects = $7, granted_by = $8, granted_at = now()
		RETURNING granted_at
	`, mapID, username, canView, canEdit, canDelete, canEditGeoObjects, canDeleteGeoObjects, grantedBy).Scan(&p.GrantedAt)
	if err != nil {
		if isPgErrCode(err, "23503") {
			return MapPermissionRecord{}, ErrMapPermissionInvalid
		}

		return MapPermissionRecord{}, fmt.Errorf("set map permission: %w", err)
	}

	s.mapPermCache.invalidate(mapPermKey{mapID: mapID, username: username})

	return p, nil
}

// DeleteMapPermission revokes username's per-map grant for mapID, if any.
func (s *Store) DeleteMapPermission(ctx context.Context, mapID uuid.UUID, username string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM map_permissions WHERE map_uuid = $1 AND username = $2`, mapID, username)
	if err != nil {
		return fmt.Errorf("delete map permission: %w", err)
	}

	s.mapPermCache.invalidate(mapPermKey{mapID: mapID, username: username})

	return nil
}
