package store

import (
	"context"
	"database/sql"
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
	// aggregate with no GROUP BY never returns zero rows) — so, unlike the
	// previous plain SELECT, there's no more ErrNoRows case, only an
	// all-NULL row when neither source has a matching grant. Each column
	// scans into sql.NullBool rather than plain bool for that reason;
	// NullBool.Bool is already false when Valid is false, which correctly
	// collapses to the same zero-value MapPermission{} the old ErrNoRows
	// branch produced.
	var canView, canEdit, canDelete, canEditGeo, canDeleteGeo sql.NullBool

	err := s.pool.QueryRow(ctx, `
		SELECT
			bool_or(can_view), bool_or(can_edit), bool_or(can_delete),
			bool_or(can_edit_geo_objects), bool_or(can_delete_geo_objects)
		FROM (
			SELECT can_view, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects
			FROM map_permissions WHERE map_uuid = $1 AND username = $2
			UNION ALL
			SELECT gmp.can_view, gmp.can_edit, gmp.can_delete, gmp.can_edit_geo_objects, gmp.can_delete_geo_objects
			FROM group_map_permissions gmp
			JOIN group_members gm ON gm.group_id = gmp.group_id
			WHERE gmp.map_uuid = $1 AND gm.username = $2
		) combined
	`, mapID, username).Scan(&canView, &canEdit, &canDelete, &canEditGeo, &canDeleteGeo)
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
	return collectRows(ctx, s.pool, "list all map permissions", `
		SELECT
			m.uuid, m.name, m.current_version, m.visible_to_all, m.anonymous_allowed, m.created_at, m.updated_at, m.created_by, m.updated_by, owner_user.username,
			mp.username, mp.can_view, mp.can_edit, mp.can_delete, mp.can_edit_geo_objects, mp.can_delete_geo_objects, mp.granted_at, mp.granted_by
		FROM map_permissions mp
		JOIN maps m ON m.uuid = mp.map_uuid
		JOIN users owner_user ON owner_user.id = m.owner_id
		ORDER BY mp.granted_at ASC
	`, func(rows pgx.Rows) (MapPermissionEntry, error) {
		var e MapPermissionEntry

		err := rows.Scan(
			&e.Map.UUID, &e.Map.Name, &e.Map.CurrentVersion, &e.Map.VisibleToAll, &e.Map.AnonymousAllowed, &e.Map.CreatedAt, &e.Map.UpdatedAt, &e.Map.CreatedBy, &e.Map.UpdatedBy, &e.Map.Owner,
			&e.Permission.Username, &e.Permission.CanView, &e.Permission.CanEdit, &e.Permission.CanDelete, &e.Permission.CanEditGeoObjects, &e.Permission.CanDeleteGeoObjects, &e.Permission.GrantedAt, &e.Permission.GrantedBy,
		)

		return e, err
	})
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
