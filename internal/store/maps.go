package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrMapNotFound is returned when a map lookup finds no matching row.
var ErrMapNotFound = errors.New("map not found")

// MapRecord is the persisted form of a map.
type MapRecord struct {
	UUID             uuid.UUID `json:"uuid"`
	Name             string    `json:"name"`
	CurrentVersion   string    `json:"currentVersion"`
	VisibleToAll     bool      `json:"visibleToAll"`
	AnonymousAllowed bool      `json:"anonymousAllowed"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
	CreatedBy        string    `json:"createdBy"`
	UpdatedBy        string    `json:"updatedBy"`
}

// CreateMap inserts a new map row with a fresh UUID, owned by createdBy.
func (s *Store) CreateMap(ctx context.Context, name, currentVersion string, visibleToAll, anonymousAllowed bool, createdBy string) (MapRecord, error) {
	m := MapRecord{
		UUID:             uuid.New(),
		Name:             name,
		CurrentVersion:   currentVersion,
		VisibleToAll:     visibleToAll,
		AnonymousAllowed: anonymousAllowed,
		CreatedBy:        createdBy,
		UpdatedBy:        createdBy,
	}

	err := s.pool.QueryRow(ctx, `
		INSERT INTO maps (uuid, name, current_version, visible_to_all, anonymous_allowed, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at
	`, m.UUID, m.Name, m.CurrentVersion, m.VisibleToAll, m.AnonymousAllowed, m.CreatedBy, m.UpdatedBy).Scan(&m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return MapRecord{}, fmt.Errorf("create map: %w", err)
	}

	if s.events != nil {
		s.events.MapCreated(m)
	}

	return m, nil
}

// MapFilter holds optional filters for ListMaps. A zero value matches every
// map the caller may otherwise see.
type MapFilter struct {
	Name             string // substring, case-insensitive
	CreatedBy        string // exact match
	VisibleToAll     *bool
	AnonymousAllowed *bool
}

// clauses returns the "column = $N"-style fragments for the filters set on
// f, binding their values through qb. Pure and DB-free so it's directly
// unit-testable.
func (f MapFilter) clauses(qb *queryBuilder) []string {
	var clauses []string
	if f.Name != "" {
		clauses = append(clauses, "name ILIKE "+qb.bind("%"+f.Name+"%"))
	}

	if f.CreatedBy != "" {
		clauses = append(clauses, "created_by = "+qb.bind(f.CreatedBy))
	}

	if f.VisibleToAll != nil {
		clauses = append(clauses, "visible_to_all = "+qb.bind(*f.VisibleToAll))
	}

	if f.AnonymousAllowed != nil {
		clauses = append(clauses, "anonymous_allowed = "+qb.bind(*f.AnonymousAllowed))
	}

	return clauses
}

// ListMaps returns every map visible to username: maps marked visible to
// all, maps username created, maps username holds a per-map view/edit/delete
// grant on, and (since they can already act on any map regardless of
// visibility) every map if bypassVisibility is true — meant to be passed as
// the acting user's is_admin || can_edit || can_delete. filter narrows the
// result further; its zero value matches everything.
func (s *Store) ListMaps(ctx context.Context, username string, bypassVisibility bool, filter MapFilter) ([]MapRecord, error) {
	qb := &queryBuilder{}
	bypassArg := qb.bind(bypassVisibility)
	userArg := qb.bind(username)

	clauses := append([]string{fmt.Sprintf(`(%s
		   OR visible_to_all
		   OR created_by = %s
		   OR EXISTS (
		        SELECT 1 FROM map_permissions mp
		        WHERE mp.map_uuid = maps.uuid AND mp.username = %s
		          AND (mp.can_view OR mp.can_edit OR mp.can_delete OR mp.can_edit_geo_objects OR mp.can_delete_geo_objects)
		      ))`, bypassArg, userArg, userArg)}, filter.clauses(qb)...)
	where := strings.Join(clauses, " AND ")

	query := fmt.Sprintf(`
		SELECT uuid, name, current_version, visible_to_all, anonymous_allowed, created_at, updated_at, created_by, updated_by
		FROM maps
		WHERE %s
		ORDER BY created_at DESC
	`, where)

	return collectRows(ctx, s.pool, "list maps", query, func(rows pgx.Rows) (MapRecord, error) {
		var m MapRecord

		err := rows.Scan(&m.UUID, &m.Name, &m.CurrentVersion, &m.VisibleToAll, &m.AnonymousAllowed, &m.CreatedAt, &m.UpdatedAt, &m.CreatedBy, &m.UpdatedBy)

		return m, err
	}, qb.args...)
}

// GetMap fetches a single map by id. It returns ErrMapNotFound if it doesn't
// exist. Results are cached for cacheTTL: this is called on every tile
// request (to check visibility/anonymousAllowed) and every map-scoped
// route, but map rows change rarely.
func (s *Store) GetMap(ctx context.Context, id uuid.UUID) (MapRecord, error) {
	if m, ok := s.mapCache.get(id); ok {
		return m, nil
	}

	var m MapRecord

	err := s.pool.QueryRow(ctx, `
		SELECT uuid, name, current_version, visible_to_all, anonymous_allowed, created_at, updated_at, created_by, updated_by
		FROM maps WHERE uuid = $1
	`, id).Scan(&m.UUID, &m.Name, &m.CurrentVersion, &m.VisibleToAll, &m.AnonymousAllowed, &m.CreatedAt, &m.UpdatedAt, &m.CreatedBy, &m.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return MapRecord{}, ErrMapNotFound
	}

	if err != nil {
		return MapRecord{}, fmt.Errorf("get map: %w", err)
	}

	s.mapCache.set(id, m)

	return m, nil
}

// GetCurrentVersion returns id's current version string. It's backed by a
// cache dedicated to this single field (separate from GetMap's MapRecord
// cache), since the version-file/bounds/geo-objects routes that resolve the
// "current" keyword only ever need this one value. It returns
// ErrMapNotFound if id doesn't exist.
func (s *Store) GetCurrentVersion(ctx context.Context, id uuid.UUID) (string, error) {
	if v, ok := s.currentVersionCache.get(id); ok {
		return v, nil
	}

	var version string

	err := s.pool.QueryRow(ctx, `SELECT current_version FROM maps WHERE uuid = $1`, id).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrMapNotFound
	}

	if err != nil {
		return "", fmt.Errorf("get current version: %w", err)
	}

	s.currentVersionCache.set(id, version)

	return version, nil
}

// UpdateMap overwrites a map's name, currentVersion, and visibility flags.
// It returns ErrMapNotFound if id doesn't exist.
func (s *Store) UpdateMap(ctx context.Context, id uuid.UUID, name, currentVersion string, visibleToAll, anonymousAllowed bool, updatedBy string) (MapRecord, error) {
	var m MapRecord

	err := s.pool.QueryRow(ctx, `
		UPDATE maps
		SET name = $2, current_version = $3, visible_to_all = $4, anonymous_allowed = $5, updated_by = $6, updated_at = now()
		WHERE uuid = $1
		RETURNING uuid, name, current_version, visible_to_all, anonymous_allowed, created_at, updated_at, created_by, updated_by
	`, id, name, currentVersion, visibleToAll, anonymousAllowed, updatedBy).Scan(&m.UUID, &m.Name, &m.CurrentVersion, &m.VisibleToAll, &m.AnonymousAllowed, &m.CreatedAt, &m.UpdatedAt, &m.CreatedBy, &m.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return MapRecord{}, ErrMapNotFound
	}

	if err != nil {
		return MapRecord{}, fmt.Errorf("update map: %w", err)
	}

	s.mapCache.invalidate(id)
	s.currentVersionCache.invalidate(id)

	return m, nil
}

// IncrementMapVersion reads the highest version recorded in map_versions for
// this map (treating no rows as 0), and atomically stores last+1 both as the
// map's current_version and as a new map_versions row. Using map_versions as
// the source of truth (rather than maps.current_version) means a PUT that
// manually edits currentVersion can't cause a later upload to reuse or
// overwrite an existing version directory.
//
// The map row lock held for the duration of the transaction serializes
// concurrent uploads for the same map so they can never be assigned the same
// version.
func (s *Store) IncrementMapVersion(ctx context.Context, id uuid.UUID, updatedBy string) (MapRecord, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MapRecord{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var exists bool

	err = tx.QueryRow(ctx, `SELECT true FROM maps WHERE uuid = $1 FOR UPDATE`, id).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return MapRecord{}, ErrMapNotFound
	}

	if err != nil {
		return MapRecord{}, fmt.Errorf("lock map: %w", err)
	}

	var lastVersion int

	err = tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(version::int), 0) FROM map_versions WHERE map_uuid = $1
	`, id).Scan(&lastVersion)
	if err != nil {
		return MapRecord{}, fmt.Errorf("get last map version: %w", err)
	}

	nextVersion := strconv.Itoa(lastVersion + 1)

	var m MapRecord

	err = tx.QueryRow(ctx, `
		UPDATE maps
		SET current_version = $2, updated_by = $3, updated_at = now()
		WHERE uuid = $1
		RETURNING uuid, name, current_version, visible_to_all, anonymous_allowed, created_at, updated_at, created_by, updated_by
	`, id, nextVersion, updatedBy).Scan(&m.UUID, &m.Name, &m.CurrentVersion, &m.VisibleToAll, &m.AnonymousAllowed, &m.CreatedAt, &m.UpdatedAt, &m.CreatedBy, &m.UpdatedBy)
	if err != nil {
		return MapRecord{}, fmt.Errorf("update map version: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO map_versions (map_uuid, version, created_by) VALUES ($1, $2, $3)
	`, id, nextVersion, updatedBy)
	if err != nil {
		return MapRecord{}, fmt.Errorf("record map version: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return MapRecord{}, fmt.Errorf("commit: %w", err)
	}

	s.mapCache.invalidate(id)
	s.currentVersionCache.invalidate(id)

	if s.events != nil {
		s.events.MapVersionUpdated(m)
	}

	return m, nil
}

// MapVersionRecord is one uploaded version in a map's history.
type MapVersionRecord struct {
	Version   string    `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	CreatedBy string    `json:"createdBy"`
}

// ListMapVersions returns the upload history for a map, most recent first.
// It returns ErrMapNotFound if the map itself doesn't exist.
func (s *Store) ListMapVersions(ctx context.Context, id uuid.UUID) ([]MapVersionRecord, error) {
	if _, err := s.GetMap(ctx, id); err != nil {
		return nil, err
	}

	return collectRows(ctx, s.pool, "list map versions", `
		SELECT version, created_at, created_by
		FROM map_versions
		WHERE map_uuid = $1
		ORDER BY created_at DESC
	`, func(rows pgx.Rows) (MapVersionRecord, error) {
		var v MapVersionRecord

		err := rows.Scan(&v.Version, &v.CreatedAt, &v.CreatedBy)

		return v, err
	}, id)
}

// DeleteMap deletes a map by id (cascading to its versions and permission
// grants). It returns ErrMapNotFound if id doesn't exist.
func (s *Store) DeleteMap(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM maps WHERE uuid = $1`, id)
	if err != nil {
		return fmt.Errorf("delete map: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrMapNotFound
	}

	s.mapCache.invalidate(id)
	s.currentVersionCache.invalidate(id)

	return nil
}
