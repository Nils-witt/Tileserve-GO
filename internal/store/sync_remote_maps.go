package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ListSyncRemoteSelectedMaps returns the admin's explicit map selection for
// remoteID — the subset of the remote's maps to mirror when its
// SyncAllMaps is false (see internal/sync.mapsToSync). An id with no rows
// simply has an empty selection, same as a freshly created remote.
func (s *Store) ListSyncRemoteSelectedMaps(ctx context.Context, remoteID uuid.UUID) ([]uuid.UUID, error) {
	return collectRows(ctx, s.pool, "list sync remote selected maps", `
		SELECT map_uuid FROM sync_remote_maps WHERE remote_id = $1 ORDER BY map_uuid
	`, func(rows pgx.Rows) (uuid.UUID, error) {
		var id uuid.UUID

		err := rows.Scan(&id)

		return id, err
	}, remoteID)
}

// SetSyncRemoteSelectedMaps replaces remoteID's explicit map selection with
// mapUUIDs, delete-then-insert within a single transaction so a sync tick
// reading the selection concurrently never observes a partially-cleared
// one. It doesn't check that remoteID exists — callers only ever invoke it
// right after a create/update of the same row, so a nonexistent id would
// only happen for an already-deleted remote, for which replacing an empty
// selection with another empty one is a harmless no-op.
func (s *Store) SetSyncRemoteSelectedMaps(ctx context.Context, remoteID uuid.UUID, mapUUIDs []uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin selected maps update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM sync_remote_maps WHERE remote_id = $1`, remoteID); err != nil {
		return fmt.Errorf("clear selected maps: %w", err)
	}

	for _, id := range mapUUIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO sync_remote_maps (remote_id, map_uuid) VALUES ($1, $2)`, remoteID, id); err != nil {
			return fmt.Errorf("insert selected map %s: %w", id, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit selected maps update: %w", err)
	}

	return nil
}
