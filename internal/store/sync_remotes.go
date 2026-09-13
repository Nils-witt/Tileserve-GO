package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
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
	ID              uuid.UUID `json:"id" gorm:"column:id;type:uuid;primaryKey"`
	Name            string    `json:"name" gorm:"column:name;not null"`
	BaseURL         string    `json:"baseUrl" gorm:"column:base_url;not null"`
	RemoteAPIKeyID  uuid.UUID `json:"remoteApiKeyId" gorm:"column:remote_api_key_id;type:uuid"`
	PollIntervalSec int       `json:"pollIntervalSec" gorm:"column:poll_interval_sec;not null"`
	Enabled         bool      `json:"enabled" gorm:"column:enabled;not null"`
	// SyncAllMaps, if true, mirrors every map visible to the configured API
	// key (the original behavior); if false, only maps present in this
	// remote's sync_remote_maps selection (see ListSyncRemoteSelectedMaps)
	// are mirrored, plus any not-yet-seen map if SyncNewMaps is also true.
	SyncAllMaps bool `json:"syncAllMaps" gorm:"column:sync_all_maps;not null"`
	// SyncNewMaps only matters when SyncAllMaps is false: it controls
	// whether a remote map never seen locally before is mirrored
	// automatically the first time it's noticed, without needing to be
	// added to the explicit selection first.
	SyncNewMaps bool `json:"syncNewMaps" gorm:"column:sync_new_maps;not null;default:false"`
	// SyncGeoObjects, if true, additionally mirrors every geo object
	// attached to each synced map's versions (see internal/sync). It's
	// independent of SyncAllMaps/SyncNewMaps, which only decide which maps
	// are synced at all.
	SyncGeoObjects bool       `json:"syncGeoObjects" gorm:"column:sync_geo_objects;not null;default:false"`
	LastSyncAt     *time.Time `json:"lastSyncAt,omitempty" gorm:"column:last_sync_at"`
	LastSyncStatus string     `json:"lastSyncStatus" gorm:"column:last_sync_status;not null;default:''"`
	LastSyncError  string     `json:"lastSyncError,omitempty" gorm:"column:last_sync_error;not null;default:''"`
	CreatedAt      time.Time  `json:"createdAt" gorm:"column:created_at;not null;default:now()"`
	UpdatedAt      time.Time  `json:"updatedAt" gorm:"column:updated_at;not null;default:now()"`
	CreatedBy      string     `json:"createdBy" gorm:"column:created_by;not null"`
	UpdatedBy      string     `json:"updatedBy" gorm:"column:updated_by;not null"`
}

// TableName implements the gorm.Tabler interface.
func (SyncRemote) TableName() string { return "sync_remotes" }

// SyncLogEntry is one line of a sync remote's recent in-memory activity log
// (see internal/sync.LogStore). Unlike everything else in this package, it
// is never persisted — it lives only in the running server's memory — but
// is declared here, rather than in package sync, so internal/webserver can
// name it in its own interface without importing internal/sync, keeping the
// handler -> sync dependency one-way (see the syncTrigger interface comment
// in internal/webserver/sync_remotes.go).
type SyncLogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
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
	sr := SyncRemote{
		ID:              uuid.New(),
		Name:            name,
		BaseURL:         baseURL,
		RemoteAPIKeyID:  remoteAPIKeyID,
		PollIntervalSec: pollIntervalSec,
		Enabled:         enabled,
		SyncAllMaps:     syncAllMaps,
		SyncNewMaps:     syncNewMaps,
		SyncGeoObjects:  syncGeoObjects,
		CreatedBy:       createdBy,
		UpdatedBy:       createdBy,
	}

	if err := s.db.WithContext(ctx).Create(&sr).Error; err != nil {
		return SyncRemote{}, fmt.Errorf("create sync remote: %w", err)
	}

	return sr, nil
}

// ListSyncRemotes returns every configured sync remote, oldest first.
func (s *Store) ListSyncRemotes(ctx context.Context) ([]SyncRemote, error) {
	remotes := []SyncRemote{}

	if err := s.db.WithContext(ctx).Order("created_at ASC").Find(&remotes).Error; err != nil {
		return nil, fmt.Errorf("list sync remotes: %w", err)
	}

	return remotes, nil
}

// GetSyncRemote fetches a single sync remote by id. It returns
// ErrSyncRemoteNotFound if it doesn't exist.
func (s *Store) GetSyncRemote(ctx context.Context, id uuid.UUID) (SyncRemote, error) {
	var sr SyncRemote

	err := s.db.WithContext(ctx).Where("id = ?", id).Take(&sr).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
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
	res := s.db.WithContext(ctx).Model(&SyncRemote{}).Where("id = ?", id).Updates(map[string]any{
		colName:             name,
		"base_url":          baseURL,
		"remote_api_key_id": remoteAPIKeyID,
		"poll_interval_sec": pollIntervalSec,
		"enabled":           enabled,
		"sync_all_maps":     syncAllMaps,
		"sync_new_maps":     syncNewMaps,
		"sync_geo_objects":  syncGeoObjects,
		colUpdatedBy:        updatedBy,
		colUpdatedAt:        gorm.Expr("now()"),
	})
	if res.Error != nil {
		return SyncRemote{}, fmt.Errorf("update sync remote: %w", res.Error)
	}

	if res.RowsAffected == 0 {
		return SyncRemote{}, ErrSyncRemoteNotFound
	}

	var sr SyncRemote
	if err := s.db.WithContext(ctx).Where("id = ?", id).Take(&sr).Error; err != nil {
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
	res := s.db.WithContext(ctx).Where("id = ?", id).Delete(&SyncRemote{})
	if res.Error != nil {
		return fmt.Errorf("delete sync remote: %w", res.Error)
	}

	if res.RowsAffected == 0 {
		return ErrSyncRemoteNotFound
	}

	return nil
}

// SetSyncRemoteStatus records the outcome of the most recent sync attempt
// for id. It's a best-effort observability write from internal/sync's
// worker loop; a nonexistent id is treated as a silent no-op (the remote
// may have been deleted mid-sync) rather than an error.
func (s *Store) SetSyncRemoteStatus(ctx context.Context, id uuid.UUID, status, errMsg string, at time.Time) error {
	err := s.db.WithContext(ctx).Model(&SyncRemote{}).Where("id = ?", id).Updates(map[string]any{
		"last_sync_at":     at,
		"last_sync_status": status,
		"last_sync_error":  errMsg,
	}).Error
	if err != nil {
		return fmt.Errorf("set sync remote status: %w", err)
	}

	return nil
}
