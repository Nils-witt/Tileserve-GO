package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

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
	return collectRows(ctx, s.pool, "list group map permissions", `
		SELECT group_id, can_view, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, granted_at, granted_by
		FROM group_map_permissions
		WHERE map_uuid = $1
		ORDER BY granted_at ASC
	`, func(rows pgx.Rows) (GroupMapPermissionRecord, error) {
		var p GroupMapPermissionRecord

		err := rows.Scan(&p.GroupID, &p.CanView, &p.CanEdit, &p.CanDelete, &p.CanEditGeoObjects, &p.CanDeleteGeoObjects, &p.GrantedAt, &p.GrantedBy)

		return p, err
	}, mapID)
}

// SetGroupMapPermission creates or replaces groupID's per-map grant for
// mapID. It returns ErrMapPermissionInvalid if mapID or groupID don't exist.
// Every cached per-map permission entry is cleared (see
// Store.UpdateGroup for why a targeted invalidation isn't possible here).
func (s *Store) SetGroupMapPermission(ctx context.Context, mapID, groupID uuid.UUID, canView, canEdit, canDelete, canEditGeoObjects, canDeleteGeoObjects bool, grantedBy string) (GroupMapPermissionRecord, error) {
	p := GroupMapPermissionRecord{
		GroupID:             groupID,
		CanView:             canView,
		CanEdit:             canEdit,
		CanDelete:           canDelete,
		CanEditGeoObjects:   canEditGeoObjects,
		CanDeleteGeoObjects: canDeleteGeoObjects,
		GrantedBy:           grantedBy,
	}

	err := s.pool.QueryRow(ctx, `
		INSERT INTO group_map_permissions (map_uuid, group_id, can_view, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, granted_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (map_uuid, group_id)
		DO UPDATE SET can_view = $3, can_edit = $4, can_delete = $5, can_edit_geo_objects = $6, can_delete_geo_objects = $7, granted_by = $8, granted_at = now()
		RETURNING granted_at
	`, mapID, groupID, canView, canEdit, canDelete, canEditGeoObjects, canDeleteGeoObjects, grantedBy).Scan(&p.GrantedAt)
	if err != nil {
		if isPgErrCode(err, "23503") {
			return GroupMapPermissionRecord{}, ErrMapPermissionInvalid
		}

		return GroupMapPermissionRecord{}, fmt.Errorf("set group map permission: %w", err)
	}

	s.mapPermCache.clear()

	return p, nil
}

// DeleteGroupMapPermission revokes groupID's per-map grant for mapID, if any.
func (s *Store) DeleteGroupMapPermission(ctx context.Context, mapID, groupID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM group_map_permissions WHERE map_uuid = $1 AND group_id = $2`, mapID, groupID)
	if err != nil {
		return fmt.Errorf("delete group map permission: %w", err)
	}

	s.mapPermCache.clear()

	return nil
}
