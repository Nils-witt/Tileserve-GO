package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	// ErrGeoObjectNotFound is returned when a geo object lookup finds no matching row.
	ErrGeoObjectNotFound = errors.New("geo object not found")
	// ErrGeoObjectInvalid is returned when creating a geo object references a map or version that does not exist.
	ErrGeoObjectInvalid = errors.New("map or version does not exist")
)

// GeoObjectRecord is a point of interest attached to a specific map version.
type GeoObjectRecord struct {
	UUID         uuid.UUID `json:"uuid"`
	MapUUID      uuid.UUID `json:"mapUuid"`
	Version      string    `json:"version"`
	Name         string    `json:"name"`
	ExternalID   string    `json:"externalId"`
	Latitude     float64   `json:"latitude"`
	Longitude    float64   `json:"longitude"`
	Street       string    `json:"street"`
	HouseNumber  string    `json:"housenumber"`
	Postcode     string    `json:"postcode"`
	City         string    `json:"city"`
	CityDistrict string    `json:"cityDistrict"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	CreatedBy    string    `json:"createdBy"`
	UpdatedBy    string    `json:"updatedBy"`
}

// CreateGeoObject inserts a new geo object row with a fresh UUID, tied to
// mapID's version. It returns ErrGeoObjectInvalid if that map/version
// combination doesn't exist in map_versions.
func (s *Store) CreateGeoObject(ctx context.Context, mapID uuid.UUID, version, name, externalID string, latitude, longitude float64, street, houseNumber, postcode, city, cityDistrict, createdBy string) (GeoObjectRecord, error) {
	g := GeoObjectRecord{
		UUID:         uuid.New(),
		MapUUID:      mapID,
		Version:      version,
		Name:         name,
		ExternalID:   externalID,
		Latitude:     latitude,
		Longitude:    longitude,
		Street:       street,
		HouseNumber:  houseNumber,
		Postcode:     postcode,
		City:         city,
		CityDistrict: cityDistrict,
		CreatedBy:    createdBy,
		UpdatedBy:    createdBy,
	}

	err := s.pool.QueryRow(ctx, `
		INSERT INTO geo_objects (uuid, map_uuid, version, name, external_id, latitude, longitude, street, housenumber, postcode, city, city_district, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING created_at, updated_at
	`, g.UUID, g.MapUUID, g.Version, g.Name, g.ExternalID, g.Latitude, g.Longitude, g.Street, g.HouseNumber, g.Postcode, g.City, g.CityDistrict, g.CreatedBy, g.UpdatedBy).Scan(&g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if isPgErrCode(err, "23503") {
			return GeoObjectRecord{}, ErrGeoObjectInvalid
		}

		return GeoObjectRecord{}, fmt.Errorf("create geo object: %w", err)
	}

	return g, nil
}

// GeoObjectFilter holds optional filters for ListGeoObjects. MinLat/MaxLat/
// MinLon/MaxLon (a bounding box) must be set together or not at all; that's
// enforced by the caller (see the handler), not here — clauses only checks
// MinLat to decide whether to emit the bbox condition.
type GeoObjectFilter struct {
	Name         string // substring, case-insensitive
	ExternalID   string // exact match
	Street       string // substring, case-insensitive
	Postcode     string // exact match
	City         string // substring, case-insensitive
	CityDistrict string // substring, case-insensitive
	CreatedBy    string // exact match

	MinLat, MaxLat, MinLon, MaxLon *float64
}

// clauses returns the "column = $N"-style fragments for the filters set on
// f, binding their values through qb. Pure and DB-free so it's directly
// unit-testable.
func (f GeoObjectFilter) clauses(qb *queryBuilder) []string {
	var clauses []string
	if f.Name != "" {
		clauses = append(clauses, "name ILIKE "+qb.bind("%"+f.Name+"%"))
	}

	if f.ExternalID != "" {
		clauses = append(clauses, "external_id = "+qb.bind(f.ExternalID))
	}

	if f.Street != "" {
		clauses = append(clauses, "street ILIKE "+qb.bind("%"+f.Street+"%"))
	}

	if f.Postcode != "" {
		clauses = append(clauses, "postcode = "+qb.bind(f.Postcode))
	}

	if f.City != "" {
		clauses = append(clauses, "city ILIKE "+qb.bind("%"+f.City+"%"))
	}

	if f.CityDistrict != "" {
		clauses = append(clauses, "city_district ILIKE "+qb.bind("%"+f.CityDistrict+"%"))
	}

	if f.CreatedBy != "" {
		clauses = append(clauses, "created_by = "+qb.bind(f.CreatedBy))
	}

	if f.MinLat != nil {
		clauses = append(clauses, fmt.Sprintf("latitude BETWEEN %s AND %s AND longitude BETWEEN %s AND %s",
			qb.bind(*f.MinLat), qb.bind(*f.MaxLat), qb.bind(*f.MinLon), qb.bind(*f.MaxLon)))
	}

	return clauses
}

// ListGeoObjects returns every geo object tied to mapID's version, oldest
// first. filter narrows the result further; its zero value matches
// everything.
func (s *Store) ListGeoObjects(ctx context.Context, mapID uuid.UUID, version string, filter GeoObjectFilter) ([]GeoObjectRecord, error) {
	qb := &queryBuilder{}
	mapArg := qb.bind(mapID)
	versionArg := qb.bind(version)

	clauses := append([]string{fmt.Sprintf("map_uuid = %s AND version = %s", mapArg, versionArg)}, filter.clauses(qb)...)
	where := strings.Join(clauses, " AND ")

	query := fmt.Sprintf(`
		SELECT uuid, map_uuid, version, name, external_id, latitude, longitude, street, housenumber, postcode, city, city_district, created_at, updated_at, created_by, updated_by
		FROM geo_objects
		WHERE %s
		ORDER BY created_at ASC
	`, where)

	return collectRows(ctx, s.pool, "list geo objects", query, func(rows pgx.Rows) (GeoObjectRecord, error) {
		var g GeoObjectRecord

		err := rows.Scan(&g.UUID, &g.MapUUID, &g.Version, &g.Name, &g.ExternalID, &g.Latitude, &g.Longitude, &g.Street, &g.HouseNumber, &g.Postcode, &g.City, &g.CityDistrict, &g.CreatedAt, &g.UpdatedAt, &g.CreatedBy, &g.UpdatedBy)

		return g, err
	}, qb.args...)
}

// GetGeoObject fetches a single geo object by id. It returns
// ErrGeoObjectNotFound if it doesn't exist.
func (s *Store) GetGeoObject(ctx context.Context, id uuid.UUID) (GeoObjectRecord, error) {
	var g GeoObjectRecord

	err := s.pool.QueryRow(ctx, `
		SELECT uuid, map_uuid, version, name, external_id, latitude, longitude, street, housenumber, postcode, city, city_district, created_at, updated_at, created_by, updated_by
		FROM geo_objects WHERE uuid = $1
	`, id).Scan(&g.UUID, &g.MapUUID, &g.Version, &g.Name, &g.ExternalID, &g.Latitude, &g.Longitude, &g.Street, &g.HouseNumber, &g.Postcode, &g.City, &g.CityDistrict, &g.CreatedAt, &g.UpdatedAt, &g.CreatedBy, &g.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return GeoObjectRecord{}, ErrGeoObjectNotFound
	}

	if err != nil {
		return GeoObjectRecord{}, fmt.Errorf("get geo object: %w", err)
	}

	return g, nil
}

// UpdateGeoObject overwrites a geo object's fields other than its map/version,
// which are immutable after creation. mapID and version scope the update to
// the caller's URL, so a mismatched id, map, or version all report as
// ErrGeoObjectNotFound in one query rather than requiring a separate lookup
// first.
func (s *Store) UpdateGeoObject(ctx context.Context, mapID uuid.UUID, version string, id uuid.UUID, name, externalID string, latitude, longitude float64, street, houseNumber, postcode, city, cityDistrict, updatedBy string) (GeoObjectRecord, error) {
	var g GeoObjectRecord

	err := s.pool.QueryRow(ctx, `
		UPDATE geo_objects
		SET name = $4, external_id = $5, latitude = $6, longitude = $7, street = $8, housenumber = $9, postcode = $10, city = $11, city_district = $12, updated_by = $13, updated_at = now()
		WHERE uuid = $1 AND map_uuid = $2 AND version = $3
		RETURNING uuid, map_uuid, version, name, external_id, latitude, longitude, street, housenumber, postcode, city, city_district, created_at, updated_at, created_by, updated_by
	`, id, mapID, version, name, externalID, latitude, longitude, street, houseNumber, postcode, city, cityDistrict, updatedBy).Scan(&g.UUID, &g.MapUUID, &g.Version, &g.Name, &g.ExternalID, &g.Latitude, &g.Longitude, &g.Street, &g.HouseNumber, &g.Postcode, &g.City, &g.CityDistrict, &g.CreatedAt, &g.UpdatedAt, &g.CreatedBy, &g.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return GeoObjectRecord{}, ErrGeoObjectNotFound
	}

	if err != nil {
		return GeoObjectRecord{}, fmt.Errorf("update geo object: %w", err)
	}

	return g, nil
}

// DeleteGeoObject deletes a geo object by id, scoped to mapID's version (see
// UpdateGeoObject). It returns ErrGeoObjectNotFound if no row matched.
func (s *Store) DeleteGeoObject(ctx context.Context, mapID uuid.UUID, version string, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM geo_objects WHERE uuid = $1 AND map_uuid = $2 AND version = $3`, id, mapID, version)
	if err != nil {
		return fmt.Errorf("delete geo object: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrGeoObjectNotFound
	}

	return nil
}
