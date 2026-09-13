package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrAPIKeyScopeInvalid is returned when granting a scope references a map that does not exist.
var ErrAPIKeyScopeInvalid = errors.New("map does not exist")

// stringArray maps a Postgres TEXT[] column to/from []string, hand-rolled
// rather than pulling in github.com/lib/pq (an otherwise-unused dependency)
// purely for this one field — api_key_scopes.versions is the only array
// column in this schema.
type stringArray []string

// Scan implements sql.Scanner, parsing a Postgres array literal like
// `{a,"b,c",d}` (NULL scans as a nil slice).
func (a *stringArray) Scan(src any) error {
	if src == nil {
		*a = nil
		return nil
	}

	var s string

	switch v := src.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("stringArray: unsupported scan type %T", src)
	}

	s = strings.TrimPrefix(strings.TrimSuffix(s, "}"), "{")
	if s == "" {
		*a = stringArray{}
		return nil
	}

	var (
		out     stringArray
		cur     strings.Builder
		quoted  bool
		escaped bool
	)

	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)

			escaped = false
		case r == '\\' && quoted:
			escaped = true
		case r == '"':
			quoted = !quoted
		case r == ',' && !quoted:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}

	out = append(out, cur.String())
	*a = out

	return nil
}

// Value implements driver.Valuer, producing a Postgres array literal (nil
// slice becomes SQL NULL).
func (a stringArray) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}

	var b strings.Builder

	b.WriteByte('{')

	for i, s := range a {
		if i > 0 {
			b.WriteByte(',')
		}

		b.WriteByte('"')

		for _, r := range s {
			if r == '"' || r == '\\' {
				b.WriteByte('\\')
			}

			b.WriteRune(r)
		}

		b.WriteByte('"')
	}

	b.WriteByte('}')

	return b.String(), nil
}

// apiKeyScopeModel is the GORM-mapped form of one row in api_key_scopes;
// APIKeyScopeRecord (the public type) omits APIKeyID since every caller
// already knows it from context.
type apiKeyScopeModel struct {
	APIKeyID  uuid.UUID   `gorm:"column:api_key_id;type:uuid;primaryKey"`
	MapUUID   uuid.UUID   `gorm:"column:map_uuid;type:uuid;primaryKey"`
	Versions  stringArray `gorm:"column:versions;type:text[]"`
	GrantedAt time.Time   `gorm:"column:granted_at;not null;default:now()"`
}

func (apiKeyScopeModel) TableName() string { return "api_key_scopes" }

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

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&APIKeyRecord{}).
			Where("id = ? AND username = ? AND revoked_at IS NULL", apiKeyID, username).
			Update("scoped", true)
		if res.Error != nil {
			return res.Error
		}

		if res.RowsAffected == 0 {
			return ErrAPIKeyNotFound
		}

		return tx.Raw(`
			INSERT INTO api_key_scopes (api_key_id, map_uuid, versions)
			VALUES (?, ?, ?)
			ON CONFLICT (api_key_id, map_uuid) DO UPDATE SET versions = ?, granted_at = now()
			RETURNING granted_at
		`, apiKeyID, mapUUID, stringArray(versions), stringArray(versions)).Row().Scan(&rec.GrantedAt)
	})
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			return APIKeyScopeRecord{}, ErrAPIKeyNotFound
		}

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
	res := s.db.WithContext(ctx).
		Where("api_key_id = ? AND map_uuid = ? AND EXISTS (SELECT 1 FROM api_keys WHERE id = ? AND username = ? AND revoked_at IS NULL)",
			apiKeyID, mapUUID, apiKeyID, username).
		Delete(&apiKeyScopeModel{})
	if res.Error != nil {
		return fmt.Errorf("delete api key scope: %w", res.Error)
	}

	if res.RowsAffected == 0 {
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
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&APIKeyRecord{}).
			Where("id = ? AND username = ? AND revoked_at IS NULL", apiKeyID, username).
			Update("scoped", false)
		if res.Error != nil {
			return res.Error
		}

		if res.RowsAffected == 0 {
			return ErrAPIKeyNotFound
		}

		return tx.Where("api_key_id = ?", apiKeyID).Delete(&apiKeyScopeModel{}).Error
	})
	if err != nil {
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
	// Scanned into apiKeyScopeModel rather than the public APIKeyScopeRecord
	// directly: Versions needs the stringArray type to correctly parse the
	// underlying text[] column, which the public type (plain []string, for
	// a clean JSON shape) doesn't carry.
	var rows []apiKeyScopeModel

	err := s.db.WithContext(ctx).Table("api_key_scopes s").
		Joins("JOIN api_keys k ON k.id = s.api_key_id").
		Where("s.api_key_id = ? AND k.username = ?", apiKeyID, username).
		Order("s.granted_at ASC").
		Select("s.map_uuid, s.versions, s.granted_at").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list api key scopes: %w", err)
	}

	scopes := make([]APIKeyScopeRecord, len(rows))
	for i, r := range rows {
		scopes[i] = APIKeyScopeRecord{MapUUID: r.MapUUID, Versions: []string(r.Versions), GrantedAt: r.GrantedAt}
	}

	return scopes, nil
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

	var versions stringArray

	err = s.db.WithContext(ctx).Raw(`SELECT versions FROM api_key_scopes WHERE api_key_id = ? AND map_uuid = ?`, apiKeyID, mapID).Row().Scan(&versions)
	if errors.Is(err, sql.ErrNoRows) {
		entry = apiKeyScopeEntry{inScope: false}
		s.apiKeyScopeCache.set(key, entry)

		return true, entry, nil
	}

	if err != nil {
		return true, apiKeyScopeEntry{}, fmt.Errorf("resolve api key map scope: %w", err)
	}

	entry = apiKeyScopeEntry{inScope: true, versions: []string(versions)}
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
