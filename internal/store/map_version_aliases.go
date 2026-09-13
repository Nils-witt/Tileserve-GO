package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrMapVersionAliasInvalid is returned when creating/updating an alias
// references a map or a version that doesn't exist in that map's
// map_versions history (an FK violation on the fk_map_version_aliases_map_version
// constraint).
var ErrMapVersionAliasInvalid = errors.New("map or version does not exist")

// ErrMapVersionAliasNotFound is returned when looking up a specific alias
// that has no row.
var ErrMapVersionAliasNotFound = errors.New("alias not found")

// mapVersionAliasModel is the GORM-mapped form of one row in
// map_version_aliases; MapVersionAlias (the public type) omits MapUUID
// since every caller already knows it from context.
type mapVersionAliasModel struct {
	MapUUID   uuid.UUID `gorm:"column:map_uuid;type:uuid;primaryKey"`
	Alias     string    `gorm:"column:alias;primaryKey"`
	Version   string    `gorm:"column:version;not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null;default:now()"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null;default:now()"`
	CreatedBy string    `gorm:"column:created_by;not null"`
	UpdatedBy string    `gorm:"column:updated_by;not null"`
}

func (mapVersionAliasModel) TableName() string { return "map_version_aliases" }

// MapVersionAlias is the persisted form of a user-defined version alias
// (e.g. "stable" -> "7"). Alongside the "current" keyword, an alias lets a
// {version} path segment resolve to a specific uploaded version without the
// caller needing to know its literal number.
type MapVersionAlias struct {
	Alias     string    `json:"alias"`
	Version   string    `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	CreatedBy string    `json:"createdBy"`
	UpdatedBy string    `json:"updatedBy"`
}

type mapAliasKey struct {
	mapID uuid.UUID
	alias string
}

// GetMapVersionAlias resolves alias to its target version for mapID. It
// returns ErrMapVersionAliasNotFound if no such alias exists for this map.
// Results are cached for cacheTTL — not the longer mapVersionTTL used by
// GetCurrentVersion, since an alias update is a deliberate user action
// ("point this name here now") where staleness works against the feature,
// unlike currentVersion which merely advances automatically on every
// upload.
func (s *Store) GetMapVersionAlias(ctx context.Context, mapID uuid.UUID, alias string) (string, error) {
	key := mapAliasKey{mapID: mapID, alias: alias}
	if v, ok := s.mapAliasCache.get(key); ok {
		return v, nil
	}

	var version string

	err := s.db.WithContext(ctx).Raw(`SELECT version FROM map_version_aliases WHERE map_uuid = ? AND alias = ?`, mapID, alias).Row().Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrMapVersionAliasNotFound
	}

	if err != nil {
		return "", fmt.Errorf("get map version alias: %w", err)
	}

	s.mapAliasCache.set(key, version)

	return version, nil
}

// ListMapVersionAliases returns every alias defined for mapID, alphabetical
// by alias.
func (s *Store) ListMapVersionAliases(ctx context.Context, mapID uuid.UUID) ([]MapVersionAlias, error) {
	aliases := []MapVersionAlias{}

	err := s.db.WithContext(ctx).Table("map_version_aliases").
		Where("map_uuid = ?", mapID).
		Order("alias ASC").
		Find(&aliases).Error
	if err != nil {
		return nil, fmt.Errorf("list map version aliases: %w", err)
	}

	return aliases, nil
}

// SetMapVersionAlias creates or replaces alias's target version for mapID.
// It returns ErrMapVersionAliasInvalid if mapID doesn't exist or version
// isn't in that map's version history. Callers must validate alias itself
// (reserved keyword / numeric-looking name) before calling this — see
// validateAliasName in internal/webserver.
func (s *Store) SetMapVersionAlias(ctx context.Context, mapID uuid.UUID, alias, version, actor string) (MapVersionAlias, error) {
	a := MapVersionAlias{Alias: alias, Version: version, CreatedBy: actor, UpdatedBy: actor}

	err := s.db.WithContext(ctx).Raw(`
		INSERT INTO map_version_aliases (map_uuid, alias, version, created_by, updated_by)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (map_uuid, alias)
		DO UPDATE SET version = ?, updated_by = ?, updated_at = now()
		RETURNING created_at, updated_at
	`, mapID, alias, version, actor, actor, version, actor).Row().Scan(&a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if isPgErrCode(err, "23503") {
			return MapVersionAlias{}, ErrMapVersionAliasInvalid
		}

		return MapVersionAlias{}, fmt.Errorf("set map version alias: %w", err)
	}

	s.mapAliasCache.invalidate(mapAliasKey{mapID: mapID, alias: alias})

	return a, nil
}

// DeleteMapVersionAlias removes alias for mapID, if it exists. It is a
// no-op (nil error) if no such alias exists.
func (s *Store) DeleteMapVersionAlias(ctx context.Context, mapID uuid.UUID, alias string) error {
	err := s.db.WithContext(ctx).
		Where("map_uuid = ? AND alias = ?", mapID, alias).
		Delete(&mapVersionAliasModel{}).Error
	if err != nil {
		return fmt.Errorf("delete map version alias: %w", err)
	}

	s.mapAliasCache.invalidate(mapAliasKey{mapID: mapID, alias: alias})

	return nil
}
