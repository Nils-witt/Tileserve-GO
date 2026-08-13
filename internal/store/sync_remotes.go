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
// internal/sync). APIKey is the credential this server presents to the
// remote — stored in plaintext, unlike every other secret in this schema,
// because it must be read back to send as a bearer token.
type SyncRemote struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	BaseURL         string     `json:"baseUrl"`
	APIKey          string     `json:"-"`
	PollIntervalSec int        `json:"pollIntervalSec"`
	Enabled         bool       `json:"enabled"`
	LastSyncAt      *time.Time `json:"lastSyncAt,omitempty"`
	LastSyncStatus  string     `json:"lastSyncStatus"`
	LastSyncError   string     `json:"lastSyncError,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	CreatedBy       string     `json:"createdBy"`
	UpdatedBy       string     `json:"updatedBy"`
}

const syncRemoteColumns = `id, name, base_url, api_key, poll_interval_sec, enabled, last_sync_at, last_sync_status, last_sync_error, created_at, updated_at, created_by, updated_by`

func scanSyncRemote(row pgx.Row) (SyncRemote, error) {
	var sr SyncRemote

	err := row.Scan(&sr.ID, &sr.Name, &sr.BaseURL, &sr.APIKey, &sr.PollIntervalSec, &sr.Enabled,
		&sr.LastSyncAt, &sr.LastSyncStatus, &sr.LastSyncError, &sr.CreatedAt, &sr.UpdatedAt, &sr.CreatedBy, &sr.UpdatedBy)

	return sr, err
}

// CreateSyncRemote registers a new remote to sync from.
func (s *Store) CreateSyncRemote(ctx context.Context, name, baseURL, apiKey string, pollIntervalSec int, enabled bool, createdBy string) (SyncRemote, error) {
	sr, err := scanSyncRemote(s.pool.QueryRow(ctx, `
		INSERT INTO sync_remotes (id, name, base_url, api_key, poll_interval_sec, enabled, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		RETURNING `+syncRemoteColumns,
		uuid.New(), name, baseURL, apiKey, pollIntervalSec, enabled, createdBy))
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

// UpdateSyncRemote overwrites a sync remote's configuration. If apiKey is
// empty, the existing key is left unchanged — mirroring UpdateUser's
// optional-password semantics — which matters because GET/list responses
// never echo the key back (see SyncRemote.APIKey's json:"-" tag), so a
// caller that only wants to flip e.g. `enabled` has no other value to send.
// It returns ErrSyncRemoteNotFound if id doesn't exist.
func (s *Store) UpdateSyncRemote(ctx context.Context, id uuid.UUID, name, baseURL, apiKey string, pollIntervalSec int, enabled bool, updatedBy string) (SyncRemote, error) {
	var (
		sr  SyncRemote
		err error
	)

	if apiKey != "" {
		sr, err = scanSyncRemote(s.pool.QueryRow(ctx, `
			UPDATE sync_remotes
			SET name = $2, base_url = $3, api_key = $4, poll_interval_sec = $5, enabled = $6, updated_by = $7, updated_at = now()
			WHERE id = $1
			RETURNING `+syncRemoteColumns,
			id, name, baseURL, apiKey, pollIntervalSec, enabled, updatedBy))
	} else {
		sr, err = scanSyncRemote(s.pool.QueryRow(ctx, `
			UPDATE sync_remotes
			SET name = $2, base_url = $3, poll_interval_sec = $4, enabled = $5, updated_by = $6, updated_at = now()
			WHERE id = $1
			RETURNING `+syncRemoteColumns,
			id, name, baseURL, pollIntervalSec, enabled, updatedBy))
	}

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
