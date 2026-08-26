package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrSyncRemoteNotFound is returned when a sync remote lookup finds no matching row.
var ErrSyncRemoteNotFound = errors.New("sync remote not found")

// SyncRemote is the persisted configuration for one remote tileserve-go
// instance this server periodically pulls a full mirror from (see
// internal/sync). RemoteAPIKeyID is the id of the API key this server's
// own persistent public key (see internal/serverkey) was registered as *on
// the remote* — not a local foreign key, since that row lives in a
// different database. Every sync remote is authenticated with that same
// server key; there is no per-remote private key to store.
type SyncRemote struct {
	ID              uuid.UUID `json:"id"`
	Name            string    `json:"name"`
	BaseURL         string    `json:"baseUrl"`
	RemoteAPIKeyID  uuid.UUID `json:"remoteApiKeyId"`
	PollIntervalSec int       `json:"pollIntervalSec"`
	Enabled         bool      `json:"enabled"`
	// SyncAllMaps, if true, mirrors every map visible to the configured API
	// key (the original behavior); if false, only maps present in this
	// remote's sync_remote_maps selection (see ListSyncRemoteSelectedMaps)
	// are mirrored, plus any not-yet-seen map if SyncNewMaps is also true.
	SyncAllMaps bool `json:"syncAllMaps"`
	// SyncNewMaps only matters when SyncAllMaps is false: it controls
	// whether a remote map never seen locally before is mirrored
	// automatically the first time it's noticed, without needing to be
	// added to the explicit selection first.
	SyncNewMaps bool `json:"syncNewMaps"`
	// SyncGeoObjects, if true, additionally mirrors every geo object
	// attached to each synced map's versions (see internal/sync). It's
	// independent of SyncAllMaps/SyncNewMaps, which only decide which maps
	// are synced at all.
	SyncGeoObjects bool       `json:"syncGeoObjects"`
	LastSyncAt     *time.Time `json:"lastSyncAt,omitempty"`
	LastSyncStatus string     `json:"lastSyncStatus"`
	LastSyncError  string     `json:"lastSyncError,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	CreatedBy      string     `json:"createdBy"`
	UpdatedBy      string     `json:"updatedBy"`
}

// SyncLogEntry is one line of a sync remote's recent in-memory activity log
// (see internal/sync.LogStore). Unlike everything else in this package, it
// is never persisted — it lives only in the running server's memory — but
// is declared here, rather than in package sync, so internal/handler can
// name it in its own interface without importing internal/sync, keeping the
// handler -> sync dependency one-way (see the syncTrigger interface comment
// in internal/handler/sync_remotes.go).
type SyncLogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

const syncRemoteColumns = `id, name, base_url, remote_api_key_id, poll_interval_sec, enabled, sync_all_maps, sync_new_maps, sync_geo_objects, last_sync_at, last_sync_status, last_sync_error, created_at, updated_at, created_by, updated_by`

func scanSyncRemote(row pgx.Row) (SyncRemote, error) {
	var sr SyncRemote

	err := row.Scan(&sr.ID, &sr.Name, &sr.BaseURL, &sr.RemoteAPIKeyID, &sr.PollIntervalSec, &sr.Enabled,
		&sr.SyncAllMaps, &sr.SyncNewMaps, &sr.SyncGeoObjects,
		&sr.LastSyncAt, &sr.LastSyncStatus, &sr.LastSyncError, &sr.CreatedAt, &sr.UpdatedAt, &sr.CreatedBy, &sr.UpdatedBy)

	return sr, err
}

// CreateSyncRemote registers a new remote to sync from. remoteAPIKeyID is
// the id of the API key this server's own persistent public key (see
// internal/serverkey) was registered as on that remote. syncAllMaps and
// syncNewMaps set the remote's initial selective-sync policy (see
// SyncRemote.SyncAllMaps/SyncNewMaps); syncGeoObjects sets
// SyncRemote.SyncGeoObjects. The explicit map selection itself is set
// separately via SetSyncRemoteSelectedMaps, once this call has returned an
// id to attach it to.
func (s *Store) CreateSyncRemote(ctx context.Context, name, baseURL string, remoteAPIKeyID uuid.UUID, pollIntervalSec int, enabled, syncAllMaps, syncNewMaps, syncGeoObjects bool, createdBy string) (SyncRemote, error) {
	sr, err := scanSyncRemote(s.pool.QueryRow(ctx, `
		INSERT INTO sync_remotes (id, name, base_url, remote_api_key_id, poll_interval_sec, enabled, sync_all_maps, sync_new_maps, sync_geo_objects, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
		RETURNING `+syncRemoteColumns,
		uuid.New(), name, baseURL, remoteAPIKeyID, pollIntervalSec, enabled, syncAllMaps, syncNewMaps, syncGeoObjects, createdBy))
	if err != nil {
		return SyncRemote{}, fmt.Errorf("create sync remote: %w", err)
	}

	return sr, nil
}

// ListSyncRemotes returns every configured sync remote, oldest first.
func (s *Store) ListSyncRemotes(ctx context.Context) ([]SyncRemote, error) {
	return collectRows(ctx, s.pool, "list sync remotes", `
		SELECT `+syncRemoteColumns+`
		FROM sync_remotes
		ORDER BY created_at ASC
	`, func(rows pgx.Rows) (SyncRemote, error) {
		return scanSyncRemote(rows)
	})
}

// GetSyncRemote fetches a single sync remote by id. It returns
// ErrSyncRemoteNotFound if it doesn't exist.
func (s *Store) GetSyncRemote(ctx context.Context, id uuid.UUID) (SyncRemote, error) {
	sr, err := scanSyncRemote(s.pool.QueryRow(ctx, `SELECT `+syncRemoteColumns+` FROM sync_remotes WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return SyncRemote{}, ErrSyncRemoteNotFound
	}

	if err != nil {
		return SyncRemote{}, fmt.Errorf("get sync remote: %w", err)
	}

	return sr, nil
}

// UpdateSyncRemote overwrites a sync remote's configuration. remoteAPIKeyID
// is always applied (it isn't secret — GET/list responses echo it back, so
// the UI always has a current value to resend). It returns
// ErrSyncRemoteNotFound if id doesn't exist. As with CreateSyncRemote, the
// explicit map selection is updated separately via
// SetSyncRemoteSelectedMaps.
func (s *Store) UpdateSyncRemote(ctx context.Context, id uuid.UUID, name, baseURL string, remoteAPIKeyID uuid.UUID, pollIntervalSec int, enabled, syncAllMaps, syncNewMaps, syncGeoObjects bool, updatedBy string) (SyncRemote, error) {
	sr, err := scanSyncRemote(s.pool.QueryRow(ctx, `
		UPDATE sync_remotes
		SET name = $2, base_url = $3, remote_api_key_id = $4, poll_interval_sec = $5, enabled = $6, sync_all_maps = $7, sync_new_maps = $8, sync_geo_objects = $9, updated_by = $10, updated_at = now()
		WHERE id = $1
		RETURNING `+syncRemoteColumns,
		id, name, baseURL, remoteAPIKeyID, pollIntervalSec, enabled, syncAllMaps, syncNewMaps, syncGeoObjects, updatedBy))

	if errors.Is(err, pgx.ErrNoRows) {
		return SyncRemote{}, ErrSyncRemoteNotFound
	}

	if err != nil {
		return SyncRemote{}, fmt.Errorf("update sync remote: %w", err)
	}

	return sr, nil
}

// DeleteSyncRemote deletes a sync remote by id. It returns
// ErrSyncRemoteNotFound if id doesn't exist. Maps previously mirrored from
// it keep their local data (maps.sync_remote_id is ON DELETE SET NULL) —
// deleting a remote stops future syncing, it never deletes already-pulled
// content.
func (s *Store) DeleteSyncRemote(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sync_remotes WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete sync remote: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrSyncRemoteNotFound
	}

	return nil
}

// SetSyncRemoteStatus records the outcome of the most recent sync attempt
// for id. It's a best-effort observability write from internal/sync's
// worker loop; a nonexistent id is treated as a silent no-op (the remote
// may have been deleted mid-sync) rather than an error.
func (s *Store) SetSyncRemoteStatus(ctx context.Context, id uuid.UUID, status, errMsg string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sync_remotes SET last_sync_at = $2, last_sync_status = $3, last_sync_error = $4 WHERE id = $1
	`, id, at, status, errMsg)
	if err != nil {
		return fmt.Errorf("set sync remote status: %w", err)
	}

	return nil
}
