package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrMapVersionAliasInvalid is returned when creating/updating an alias
// references a map or a version that doesn't exist in that map's
// map_versions history (an FK violation on map_version_aliases' composite
// FK to map_versions).
var ErrMapVersionAliasInvalid = errors.New("map or version does not exist")

// ErrMapVersionAliasNotFound is returned when looking up a specific alias
// that has no row.
var ErrMapVersionAliasNotFound = errors.New("alias not found")

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

	err := s.pool.QueryRow(ctx, `
		SELECT version FROM map_version_aliases WHERE map_uuid = $1 AND alias = $2
	`, mapID, alias).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
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
	return collectRows(ctx, s.pool, "list map version aliases", `
		SELECT alias, version, created_at, updated_at, created_by, updated_by
		FROM map_version_aliases
		WHERE map_uuid = $1
		ORDER BY alias ASC
	`, func(rows pgx.Rows) (MapVersionAlias, error) {
		var a MapVersionAlias

		err := rows.Scan(&a.Alias, &a.Version, &a.CreatedAt, &a.UpdatedAt, &a.CreatedBy, &a.UpdatedBy)

		return a, err
	}, mapID)
}

// SetMapVersionAlias creates or replaces alias's target version for mapID.
// It returns ErrMapVersionAliasInvalid if mapID doesn't exist or version
// isn't in that map's version history. Callers must validate alias itself
// (reserved keyword / numeric-looking name) before calling this — see
// validateAliasName in internal/handler.
func (s *Store) SetMapVersionAlias(ctx context.Context, mapID uuid.UUID, alias, version, actor string) (MapVersionAlias, error) {
	a := MapVersionAlias{Alias: alias, Version: version, CreatedBy: actor, UpdatedBy: actor}

	err := s.pool.QueryRow(ctx, `
		INSERT INTO map_version_aliases (map_uuid, alias, version, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $4)
		ON CONFLICT (map_uuid, alias)
		DO UPDATE SET version = $3, updated_by = $4, updated_at = now()
		RETURNING created_at, updated_at
	`, mapID, alias, version, actor).Scan(&a.CreatedAt, &a.UpdatedAt)
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
	_, err := s.pool.Exec(ctx, `DELETE FROM map_version_aliases WHERE map_uuid = $1 AND alias = $2`, mapID, alias)
	if err != nil {
		return fmt.Errorf("delete map version alias: %w", err)
	}

	s.mapAliasCache.invalidate(mapAliasKey{mapID: mapID, alias: alias})

	return nil
}
