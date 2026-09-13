package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// syncRemoteMapModel is the GORM-mapped form of one row in
// sync_remote_maps — there's no public record type for it since callers
// only ever see the plain []uuid.UUID selection (see
// ListSyncRemoteSelectedMaps).
type syncRemoteMapModel struct {
	RemoteID  uuid.UUID `gorm:"column:remote_id;type:uuid;primaryKey"`
	MapUUID   uuid.UUID `gorm:"column:map_uuid;type:uuid;primaryKey"`
	CreatedAt time.Time `gorm:"column:created_at;not null;default:now()"`
}

func (syncRemoteMapModel) TableName() string { return "sync_remote_maps" }

// ListSyncRemoteSelectedMaps returns the admin's explicit map selection for
// remoteID — the subset of the remote's maps to mirror when its
// SyncAllMaps is false (see internal/sync.mapsToSync). An id with no rows
// simply has an empty selection, same as a freshly created remote.
func (s *Store) ListSyncRemoteSelectedMaps(ctx context.Context, remoteID uuid.UUID) ([]uuid.UUID, error) {
	ids := []uuid.UUID{}

	err := s.db.WithContext(ctx).Model(&syncRemoteMapModel{}).
		Where("remote_id = ?", remoteID).
		Order("map_uuid").
		Pluck("map_uuid", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("list sync remote selected maps: %w", err)
	}

	return ids, nil
}

// SetSyncRemoteSelectedMaps replaces remoteID's explicit map selection with
// mapUUIDs, delete-then-insert within a single transaction so a sync tick
// reading the selection concurrently never observes a partially-cleared
// one. It doesn't check that remoteID exists — callers only ever invoke it
// right after a create/update of the same row, so a nonexistent id would
// only happen for an already-deleted remote, for which replacing an empty
// selection with another empty one is a harmless no-op.
func (s *Store) SetSyncRemoteSelectedMaps(ctx context.Context, remoteID uuid.UUID, mapUUIDs []uuid.UUID) error {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("remote_id = ?", remoteID).Delete(&syncRemoteMapModel{}).Error; err != nil {
			return fmt.Errorf("clear selected maps: %w", err)
		}

		for _, id := range mapUUIDs {
			if err := tx.Create(&syncRemoteMapModel{RemoteID: remoteID, MapUUID: id}).Error; err != nil {
				return fmt.Errorf("insert selected map %s: %w", id, err)
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("update selected maps: %w", err)
	}

	return nil
}
