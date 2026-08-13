package store

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrSyncRemoteNotFound is returned when a sync remote lookup finds no matching row.
var ErrSyncRemoteNotFound = errors.New("sync remote not found")

// ErrInvalidPrivateKeyPEM is returned when a submitted private key isn't a
// PEM-encoded RSA private key of at least minRSAKeyBits.
var ErrInvalidPrivateKeyPEM = errors.New("private key must be a PEM-encoded RSA private key of at least 2048 bits")

// SyncRemote is the persisted configuration for one remote tileserve-go
// instance this server periodically pulls a full mirror from (see
// internal/sync). RemoteAPIKeyID is the id of the API key this server's
// public key was registered as *on the remote* — not a local foreign key,
// since that row lives in a different database. PrivateKeyPEM is this
// server's own RSA private key, used to sign the short-lived JWTs it
// presents to the remote; it's stored in plaintext, unlike every other
// secret in this schema, because it must be read back to sign each outbound
// request.
type SyncRemote struct {
	ID              uuid.UUID `json:"id"`
	Name            string    `json:"name"`
	BaseURL         string    `json:"baseUrl"`
	RemoteAPIKeyID  uuid.UUID `json:"remoteApiKeyId"`
	PrivateKeyPEM   string    `json:"-"`
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
	SyncNewMaps    bool       `json:"syncNewMaps"`
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

const syncRemoteColumns = `id, name, base_url, remote_api_key_id, private_key_pem, poll_interval_sec, enabled, sync_all_maps, sync_new_maps, last_sync_at, last_sync_status, last_sync_error, created_at, updated_at, created_by, updated_by`

func scanSyncRemote(row pgx.Row) (SyncRemote, error) {
	var sr SyncRemote

	err := row.Scan(&sr.ID, &sr.Name, &sr.BaseURL, &sr.RemoteAPIKeyID, &sr.PrivateKeyPEM, &sr.PollIntervalSec, &sr.Enabled,
		&sr.SyncAllMaps, &sr.SyncNewMaps,
		&sr.LastSyncAt, &sr.LastSyncStatus, &sr.LastSyncError, &sr.CreatedAt, &sr.UpdatedAt, &sr.CreatedBy, &sr.UpdatedBy)

	return sr, err
}

// validateRSAPrivateKeyPEM parses pemStr as an RSA private key (PKCS8 or
// PKCS1, mirroring jwt.ParseRSAPrivateKeyFromPEM's own fallback order) and
// rejects anything under minRSAKeyBits or that fails its own consistency
// check.
func validateRSAPrivateKeyPEM(pemStr string) error {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return ErrInvalidPrivateKeyPEM
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidPrivateKeyPEM, err)
		}
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok || rsaKey.N.BitLen() < minRSAKeyBits {
		return ErrInvalidPrivateKeyPEM
	}

	if err := rsaKey.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidPrivateKeyPEM, err)
	}

	return nil
}

// CreateSyncRemote registers a new remote to sync from. privateKeyPEM is
// this server's own key, used to sign requests to the remote; remoteAPIKeyID
// is the id of the API key the matching public half was registered as on
// that remote. syncAllMaps and syncNewMaps set the remote's initial
// selective-sync policy (see SyncRemote.SyncAllMaps/SyncNewMaps); the
// explicit map selection itself is set separately via
// SetSyncRemoteSelectedMaps, once this call has returned an id to attach it
// to.
func (s *Store) CreateSyncRemote(ctx context.Context, name, baseURL string, remoteAPIKeyID uuid.UUID, privateKeyPEM string, pollIntervalSec int, enabled, syncAllMaps, syncNewMaps bool, createdBy string) (SyncRemote, error) {
	if err := validateRSAPrivateKeyPEM(privateKeyPEM); err != nil {
		return SyncRemote{}, err
	}

	sr, err := scanSyncRemote(s.pool.QueryRow(ctx, `
		INSERT INTO sync_remotes (id, name, base_url, remote_api_key_id, private_key_pem, poll_interval_sec, enabled, sync_all_maps, sync_new_maps, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
		RETURNING `+syncRemoteColumns,
		uuid.New(), name, baseURL, remoteAPIKeyID, privateKeyPEM, pollIntervalSec, enabled, syncAllMaps, syncNewMaps, createdBy))
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
// the UI always has a current value to resend). If privateKeyPEM is empty,
// the existing key is left unchanged — mirroring UpdateUser's
// optional-password semantics — which matters because GET/list responses
// never echo the key back (see SyncRemote.PrivateKeyPEM's json:"-" tag), so
// a caller that only wants to flip e.g. `enabled` has no other value to
// send. It returns ErrSyncRemoteNotFound if id doesn't exist. As with
// CreateSyncRemote, the explicit map selection is updated separately via
// SetSyncRemoteSelectedMaps.
func (s *Store) UpdateSyncRemote(ctx context.Context, id uuid.UUID, name, baseURL string, remoteAPIKeyID uuid.UUID, privateKeyPEM string, pollIntervalSec int, enabled, syncAllMaps, syncNewMaps bool, updatedBy string) (SyncRemote, error) {
	var (
		sr  SyncRemote
		err error
	)

	if privateKeyPEM != "" {
		if err := validateRSAPrivateKeyPEM(privateKeyPEM); err != nil {
			return SyncRemote{}, err
		}

		sr, err = scanSyncRemote(s.pool.QueryRow(ctx, `
			UPDATE sync_remotes
			SET name = $2, base_url = $3, remote_api_key_id = $4, private_key_pem = $5, poll_interval_sec = $6, enabled = $7, sync_all_maps = $8, sync_new_maps = $9, updated_by = $10, updated_at = now()
			WHERE id = $1
			RETURNING `+syncRemoteColumns,
			id, name, baseURL, remoteAPIKeyID, privateKeyPEM, pollIntervalSec, enabled, syncAllMaps, syncNewMaps, updatedBy))
	} else {
		sr, err = scanSyncRemote(s.pool.QueryRow(ctx, `
			UPDATE sync_remotes
			SET name = $2, base_url = $3, remote_api_key_id = $4, poll_interval_sec = $5, enabled = $6, sync_all_maps = $7, sync_new_maps = $8, updated_by = $9, updated_at = now()
			WHERE id = $1
			RETURNING `+syncRemoteColumns,
			id, name, baseURL, remoteAPIKeyID, pollIntervalSec, enabled, syncAllMaps, syncNewMaps, updatedBy))
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
