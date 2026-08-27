package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// UpsertSyncedMap creates or overwrites a locally-mirrored map under the
// remote's own UUID (never a fresh uuid.New(), unlike CreateMap) — the
// simplest possible full mirror, with no separate ID-mapping table needed.
// Every sync tick re-applies name/visibility from the remote, so a synced
// map's visibility is not independently editable locally. current_version
// is deliberately left untouched here; see SetSyncedCurrentVersion.
func (s *Store) UpsertSyncedMap(ctx context.Context, id uuid.UUID, name string, visibleToAll, anonymousAllowed bool, syncRemoteID uuid.UUID, actor string) (MapRecord, error) {
	var m MapRecord

	// owner_id is deliberately absent from the ON CONFLICT SET list: it's
	// resolved from actor on first insert only, so a later local ownership
	// transfer (see UpdateMapOwner) survives subsequent sync ticks
	// re-applying name/visibility from the remote. RETURNING can't reach
	// across the join needed to resolve owner_id back to a username (a
	// plain INSERT/ON CONFLICT RETURNING only sees the target table), so
	// the upsert is wrapped in a CTE and the join happens in the outer
	// SELECT instead.
	err := scanMap(s.pool.QueryRow(ctx, `
		WITH upserted AS (
			INSERT INTO maps (uuid, name, current_version, visible_to_all, anonymous_allowed, sync_remote_id, created_by, updated_by, owner_id)
			VALUES ($1, $2, '', $3, $4, $5, $6, $6, (SELECT id FROM users WHERE username = $6))
			ON CONFLICT (uuid) DO UPDATE
			SET name = $2, visible_to_all = $3, anonymous_allowed = $4, sync_remote_id = $5, updated_by = $6, updated_at = now()
			RETURNING *
		)
		SELECT upserted.uuid, upserted.name, upserted.current_version, upserted.visible_to_all, upserted.anonymous_allowed, upserted.created_at, upserted.updated_at, upserted.created_by, upserted.updated_by, owner_user.username
		FROM upserted JOIN users owner_user ON owner_user.id = upserted.owner_id
	`, id, name, visibleToAll, anonymousAllowed, syncRemoteID, actor), &m)
	if err != nil {
		return MapRecord{}, fmt.Errorf("upsert synced map: %w", err)
	}

	s.mapCache.invalidate(id)
	s.currentVersionCache.invalidate(id)

	return m, nil
}

// HasMapVersion reports whether version is already recorded locally for id.
// Checked BEFORE downloading a version's archive, so a sync resumed after a
// crash or restart never redundantly re-pulls an already-synced version.
func (s *Store) HasMapVersion(ctx context.Context, id uuid.UUID, version string) (bool, error) {
	var exists bool

	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM map_versions WHERE map_uuid = $1 AND version = $2)
	`, id, version).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check map version: %w", err)
	}

	return exists, nil
}

// RecordSyncedMapVersion idempotently records a specific, caller-supplied
// version for id — unlike IncrementMapVersion, which always computes and
// assigns the next auto-incrementing version, this preserves the remote's
// original version number, createdAt, and createdBy for historical
// fidelity. It is a no-op if the version is already recorded (ON CONFLICT
// DO NOTHING), so it's safe to call again after a crash mid-sync.
func (s *Store) RecordSyncedMapVersion(ctx context.Context, id uuid.UUID, version, createdBy string, createdAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO map_versions (map_uuid, version, created_by, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (map_uuid, version) DO NOTHING
	`, id, version, createdBy, createdAt)
	if err != nil {
		return fmt.Errorf("record synced map version: %w", err)
	}

	return nil
}

// ErrSyncedVersionNotRecorded is returned by SetSyncedCurrentVersion when
// version hasn't been recorded locally yet (via RecordSyncedMapVersion) —
// current_version must never point at a version that isn't actually synced.
var ErrSyncedVersionNotRecorded = errors.New("version not recorded locally")

// SetSyncedCurrentVersion advances id's current_version to version, but only
// if version is already present in its local map_versions history —
// otherwise it returns ErrSyncedVersionNotRecorded without changing
// anything. This lets the sync worker safely call it once a map's versions
// have been pulled, even if the remote's own current_version races ahead of
// what's been fully downloaded: that just gets picked up on a later tick.
func (s *Store) SetSyncedCurrentVersion(ctx context.Context, id uuid.UUID, version string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE maps SET current_version = $2, updated_at = now()
		WHERE uuid = $1 AND EXISTS (
			SELECT 1 FROM map_versions WHERE map_uuid = $1 AND version = $2
		)
	`, id, version)
	if err != nil {
		return fmt.Errorf("set synced current version: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrSyncedVersionNotRecorded
	}

	s.mapCache.invalidate(id)
	s.currentVersionCache.invalidate(id)

	return nil
}
