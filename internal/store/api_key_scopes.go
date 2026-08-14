package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrAPIKeyScopeInvalid is returned when granting a scope references a map that does not exist.
var ErrAPIKeyScopeInvalid = errors.New("map does not exist")

// APIKeyScopeRecord is the persisted form of one map's scope grant for an
// API key. An empty Versions means every version of MapUUID is in scope.
type APIKeyScopeRecord struct {
	MapUUID   uuid.UUID `json:"mapUuid"`
	Versions  []string  `json:"versions,omitempty"`
	GrantedAt time.Time `json:"grantedAt"`
}

// apiKeyScopeEntry is the cached form of one map's scope entry for a key
// that is known to be scoped (see apiKeyScopedFlag) — resolveAPIKeyMapScope
// only ever populates this once the key's scoped flag is true.
type apiKeyScopeEntry struct {
	inScope  bool
	versions []string // empty = every version of this map is allowed
}

// SetAPIKeyScope grants apiKeyID access to mapUUID, restricted to versions
// (empty means every version), and marks the key as scoped. It verifies
// apiKeyID belongs to username and isn't revoked, returning ErrAPIKeyNotFound
// otherwise, or ErrAPIKeyScopeInvalid if mapUUID doesn't exist.
func (s *Store) SetAPIKeyScope(ctx context.Context, username string, apiKeyID, mapUUID uuid.UUID, versions []string) (APIKeyScopeRecord, error) {
	rec := APIKeyScopeRecord{MapUUID: mapUUID, Versions: versions}

	err := s.pool.QueryRow(ctx, `
		WITH key_check AS (
			UPDATE api_keys SET scoped = true
			WHERE id = $1 AND username = $4 AND revoked_at IS NULL
			RETURNING id
		)
		INSERT INTO api_key_scopes (api_key_id, map_uuid, versions)
		SELECT $1, $2, $3 FROM key_check
		ON CONFLICT (api_key_id, map_uuid) DO UPDATE SET versions = $3, granted_at = now()
		RETURNING granted_at
	`, apiKeyID, mapUUID, versions, username).Scan(&rec.GrantedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return APIKeyScopeRecord{}, ErrAPIKeyNotFound
	}

	if err != nil {
		if isPgErrCode(err, "23503") {
			return APIKeyScopeRecord{}, ErrAPIKeyScopeInvalid
		}

		return APIKeyScopeRecord{}, fmt.Errorf("set api key scope: %w", err)
	}

	s.apiKeyCache.invalidate(apiKeyID)
	s.apiKeyScopeCache.invalidate(apiKeyScopeKey{apiKeyID: apiKeyID, mapUUID: mapUUID})

	return rec, nil
}

// DeleteAPIKeyScope removes mapUUID from apiKeyID's scope, if apiKeyID
// belongs to username. It leaves the key's scoped flag untouched — removing
// one map only ever narrows access further, never restores unrestricted
// access. It returns ErrAPIKeyNotFound if the key doesn't belong to username
// or there was no such scope entry.
func (s *Store) DeleteAPIKeyScope(ctx context.Context, username string, apiKeyID, mapUUID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM api_key_scopes
		WHERE api_key_id = $1 AND map_uuid = $2
		  AND EXISTS (SELECT 1 FROM api_keys WHERE id = $1 AND username = $3 AND revoked_at IS NULL)
	`, apiKeyID, mapUUID, username)
	if err != nil {
		return fmt.Errorf("delete api key scope: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}

	s.apiKeyScopeCache.invalidate(apiKeyScopeKey{apiKeyID: apiKeyID, mapUUID: mapUUID})

	return nil
}

// ClearAPIKeyScope removes every scope entry for apiKeyID and resets its
// scoped flag to false, restoring unrestricted access — the explicit
// counterpart to DeleteAPIKeyScope's per-map narrowing. It returns
// ErrAPIKeyNotFound if apiKeyID doesn't belong to username.
func (s *Store) ClearAPIKeyScope(ctx context.Context, username string, apiKeyID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE api_keys SET scoped = false
		WHERE id = $1 AND username = $2 AND revoked_at IS NULL
	`, apiKeyID, username)
	if err != nil {
		return fmt.Errorf("clear api key scope: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}

	if _, err := s.pool.Exec(ctx, `DELETE FROM api_key_scopes WHERE api_key_id = $1`, apiKeyID); err != nil {
		return fmt.Errorf("clear api key scope: %w", err)
	}

	// Per-map apiKeyScopeCache entries for this key are left to expire
	// naturally within cacheTTL: resolveAPIKeyMapScope always checks the
	// (now-invalidated) scoped flag first and short-circuits to
	// unrestricted before ever consulting them.
	s.apiKeyCache.invalidate(apiKeyID)

	return nil
}

// ListAPIKeyScopes returns every scope entry for apiKeyID, oldest first. It
// returns an empty slice if apiKeyID doesn't belong to username or has no
// scope entries.
func (s *Store) ListAPIKeyScopes(ctx context.Context, username string, apiKeyID uuid.UUID) ([]APIKeyScopeRecord, error) {
	return collectRows(ctx, s.pool, "list api key scopes", `
		SELECT s.map_uuid, s.versions, s.granted_at
		FROM api_key_scopes s
		JOIN api_keys k ON k.id = s.api_key_id
		WHERE s.api_key_id = $1 AND k.username = $2
		ORDER BY s.granted_at ASC
	`, func(rows pgx.Rows) (APIKeyScopeRecord, error) {
		var r APIKeyScopeRecord

		err := rows.Scan(&r.MapUUID, &r.Versions, &r.GrantedAt)

		return r, err
	}, apiKeyID, username)
}

// resolveAPIKeyMapScope reports whether apiKeyID is scope-restricted at all
// (scoped), and if so, its cached or freshly queried scope entry for mapID.
// When scoped is false, entry is meaningless — callers must treat an
// unscoped key as unrestricted before looking at entry.
func (s *Store) resolveAPIKeyMapScope(ctx context.Context, apiKeyID, mapID uuid.UUID) (scoped bool, entry apiKeyScopeEntry, err error) {
	scoped, err = s.apiKeyScopedFlag(ctx, apiKeyID)
	if err != nil || !scoped {
		return scoped, apiKeyScopeEntry{}, err
	}

	key := apiKeyScopeKey{apiKeyID: apiKeyID, mapUUID: mapID}
	if e, ok := s.apiKeyScopeCache.get(key); ok {
		return true, e, nil
	}

	var versions []string

	err = s.pool.QueryRow(ctx, `
		SELECT versions FROM api_key_scopes WHERE api_key_id = $1 AND map_uuid = $2
	`, apiKeyID, mapID).Scan(&versions)
	if errors.Is(err, pgx.ErrNoRows) {
		entry = apiKeyScopeEntry{inScope: false}
		s.apiKeyScopeCache.set(key, entry)

		return true, entry, nil
	}

	if err != nil {
		return true, apiKeyScopeEntry{}, fmt.Errorf("resolve api key map scope: %w", err)
	}

	entry = apiKeyScopeEntry{inScope: true, versions: versions}
	s.apiKeyScopeCache.set(key, entry)

	return true, entry, nil
}

// APIKeyCanAccessMap reports whether apiKeyID may access mapID at all: true
// if the key is unscoped, or if it's scoped and mapID is in its scope.
func (s *Store) APIKeyCanAccessMap(ctx context.Context, apiKeyID, mapID uuid.UUID) (bool, error) {
	scoped, entry, err := s.resolveAPIKeyMapScope(ctx, apiKeyID, mapID)
	if err != nil {
		return false, err
	}

	return !scoped || entry.inScope, nil
}

// APIKeyCanAccessMapVersion reports whether apiKeyID may access version of
// mapID: true if the key is unscoped, or if it's scoped, mapID is in its
// scope, and either the scope entry has no version whitelist or version is
// in it.
func (s *Store) APIKeyCanAccessMapVersion(ctx context.Context, apiKeyID, mapID uuid.UUID, version string) (bool, error) {
	scoped, entry, err := s.resolveAPIKeyMapScope(ctx, apiKeyID, mapID)
	if err != nil {
		return false, err
	}

	if !scoped {
		return true, nil
	}

	if !entry.inScope {
		return false, nil
	}

	if len(entry.versions) == 0 {
		return true, nil
	}

	return slices.Contains(entry.versions, version), nil
}

// APIKeyScopedVersions returns apiKeyID's version whitelist for mapID, for
// filtering a version listing. restricted is false (versions nil) when
// every version should be shown: the key is unscoped, mapID isn't in its
// scope (the caller shouldn't be listing it at all), or the scope entry for
// mapID carries no version whitelist.
func (s *Store) APIKeyScopedVersions(ctx context.Context, apiKeyID, mapID uuid.UUID) (versions []string, restricted bool, err error) {
	scoped, entry, err := s.resolveAPIKeyMapScope(ctx, apiKeyID, mapID)
	if err != nil {
		return nil, false, err
	}

	if !scoped || !entry.inScope || len(entry.versions) == 0 {
		return nil, false, nil
	}

	return entry.versions, true, nil
}
