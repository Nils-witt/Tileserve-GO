package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	// ErrGeoObjectNotFound is returned when a geo object lookup finds no matching row.
	ErrGeoObjectNotFound = errors.New("geo object not found")
	// ErrGeoObjectInvalid is returned when creating a geo object references a map or version that does not exist.
	ErrGeoObjectInvalid = errors.New("map or version does not exist")
)

// GeoObjectRecord is a point of interest attached to a specific map version.
type GeoObjectRecord struct {
	UUID       uuid.UUID `json:"uuid" gorm:"column:uuid;type:uuid;primaryKey"`
	MapUUID    uuid.UUID `json:"mapUuid" gorm:"column:map_uuid;type:uuid;not null;index:idx_geo_objects_map_version"`
	Version    string    `json:"version" gorm:"column:version;not null;index:idx_geo_objects_map_version"`
	Name       string    `json:"name" gorm:"column:name;not null"`
	ExternalID string    `json:"externalId" gorm:"column:external_id;not null;default:''"`
	Latitude   float64   `json:"latitude" gorm:"column:latitude;not null"`
	Longitude  float64   `json:"longitude" gorm:"column:longitude;not null"`
	Street     string    `json:"street" gorm:"column:street;not null;default:''"`
	// HouseNumber persists as "housenumber" (no underscore) — the one
	// column in this schema whose name doesn't match its Go field's default
	// snake_case conversion, so it needs an explicit column tag.
	HouseNumber  string    `json:"housenumber" gorm:"column:housenumber;not null;default:''"`
	Postcode     string    `json:"postcode" gorm:"column:postcode;not null;default:''"`
	City         string    `json:"city" gorm:"column:city;not null;default:''"`
	CityDistrict string    `json:"cityDistrict" gorm:"column:city_district;not null;default:''"`
	CreatedAt    time.Time `json:"createdAt" gorm:"column:created_at;not null;default:now()"`
	UpdatedAt    time.Time `json:"updatedAt" gorm:"column:updated_at;not null;default:now()"`
	CreatedBy    string    `json:"createdBy" gorm:"column:created_by;not null"`
	UpdatedBy    string    `json:"updatedBy" gorm:"column:updated_by;not null"`
}

// TableName implements the gorm.Tabler interface.
func (GeoObjectRecord) TableName() string { return "geo_objects" }

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

	if err := s.db.WithContext(ctx).Create(&g).Error; err != nil {
		if isPgErrCode(err, "23503") {
			return GeoObjectRecord{}, ErrGeoObjectInvalid
		}

		return GeoObjectRecord{}, fmt.Errorf("create geo object: %w", err)
	}

	return g, nil
}

// GeoObjectFilter holds optional filters for ListGeoObjects. MinLat/MaxLat/
// MinLon/MaxLon (a bounding box) must be set together or not at all; that's
// enforced by the caller (see the handler), not here — Scope only checks
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

// Scope applies f's set filters to db as additional WHERE conditions.
func (f GeoObjectFilter) Scope() func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if f.Name != "" {
			db = db.Where("name ILIKE ?", "%"+f.Name+"%")
		}

		if f.ExternalID != "" {
			db = db.Where("external_id = ?", f.ExternalID)
		}

		if f.Street != "" {
			db = db.Where("street ILIKE ?", "%"+f.Street+"%")
		}

		if f.Postcode != "" {
			db = db.Where("postcode = ?", f.Postcode)
		}

		if f.City != "" {
			db = db.Where("city ILIKE ?", "%"+f.City+"%")
		}

		if f.CityDistrict != "" {
			db = db.Where("city_district ILIKE ?", "%"+f.CityDistrict+"%")
		}

		if f.CreatedBy != "" {
			db = db.Where("created_by = ?", f.CreatedBy)
		}

		if f.MinLat != nil {
			db = db.Where("latitude BETWEEN ? AND ? AND longitude BETWEEN ? AND ?", *f.MinLat, *f.MaxLat, *f.MinLon, *f.MaxLon)
		}

		return db
	}
}

// ListGeoObjects returns every geo object tied to mapID's version, oldest
// first. filter narrows the result further; its zero value matches
// everything.
func (s *Store) ListGeoObjects(ctx context.Context, mapID uuid.UUID, version string, filter GeoObjectFilter) ([]GeoObjectRecord, error) {
	objects := []GeoObjectRecord{}

	err := s.db.WithContext(ctx).
		Where("map_uuid = ? AND version = ?", mapID, version).
		Scopes(filter.Scope()).
		Order("created_at ASC").
		Find(&objects).Error
	if err != nil {
		return nil, fmt.Errorf("list geo objects: %w", err)
	}

	return objects, nil
}

// GetGeoObject fetches a single geo object by id. It returns
// ErrGeoObjectNotFound if it doesn't exist.
func (s *Store) GetGeoObject(ctx context.Context, id uuid.UUID) (GeoObjectRecord, error) {
	var g GeoObjectRecord

	err := s.db.WithContext(ctx).Where("uuid = ?", id).Take(&g).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
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
	res := s.db.WithContext(ctx).Model(&GeoObjectRecord{}).
		Where("uuid = ? AND map_uuid = ? AND version = ?", id, mapID, version).
		Updates(map[string]any{
			colName:         name,
			"external_id":   externalID,
			"latitude":      latitude,
			"longitude":     longitude,
			"street":        street,
			"housenumber":   houseNumber,
			"postcode":      postcode,
			"city":          city,
			"city_district": cityDistrict,
			colUpdatedBy:    updatedBy,
			colUpdatedAt:    gorm.Expr("now()"),
		})
	if res.Error != nil {
		return GeoObjectRecord{}, fmt.Errorf("update geo object: %w", res.Error)
	}

	if res.RowsAffected == 0 {
		return GeoObjectRecord{}, ErrGeoObjectNotFound
	}

	var g GeoObjectRecord
	if err := s.db.WithContext(ctx).Where("uuid = ?", id).Take(&g).Error; err != nil {
		return GeoObjectRecord{}, fmt.Errorf("update geo object: %w", err)
	}

	return g, nil
}

// DeleteGeoObject deletes a geo object by id, scoped to mapID's version (see
// UpdateGeoObject). It returns ErrGeoObjectNotFound if no row matched.
func (s *Store) DeleteGeoObject(ctx context.Context, mapID uuid.UUID, version string, id uuid.UUID) error {
	res := s.db.WithContext(ctx).Where("uuid = ? AND map_uuid = ? AND version = ?", id, mapID, version).Delete(&GeoObjectRecord{})
	if res.Error != nil {
		return fmt.Errorf("delete geo object: %w", res.Error)
	}

	if res.RowsAffected == 0 {
		return ErrGeoObjectNotFound
	}

	return nil
}
