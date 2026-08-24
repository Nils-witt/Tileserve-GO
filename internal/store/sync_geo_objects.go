package store

import (
	"context"
	"fmt"
)

// UpsertSyncedGeoObject creates or overwrites a locally-mirrored geo object
// under the remote's own UUID (never uuid.New(), matching UpsertSyncedMap),
// preserving the remote's CreatedAt/UpdatedAt/CreatedBy/UpdatedBy verbatim
// for historical fidelity rather than resetting them to now()/the sync actor
// — mirroring RecordSyncedMapVersion's philosophy for versions. Takes the
// whole record rather than positional params (unlike CreateGeoObject):
// the caller already has one fully formed from Client.ListGeoObjects.
// map_uuid/version are only ever set on insert, never on conflict, since
// they're immutable after creation (see UpdateGeoObject).
func (s *Store) UpsertSyncedGeoObject(ctx context.Context, g GeoObjectRecord) (GeoObjectRecord, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO geo_objects (uuid, map_uuid, version, name, external_id, latitude, longitude, street, housenumber, postcode, city, city_district, created_at, updated_at, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (uuid) DO UPDATE
		SET name = $4, external_id = $5, latitude = $6, longitude = $7, street = $8, housenumber = $9, postcode = $10, city = $11, city_district = $12, updated_at = $14, updated_by = $16
		RETURNING uuid, map_uuid, version, name, external_id, latitude, longitude, street, housenumber, postcode, city, city_district, created_at, updated_at, created_by, updated_by
	`, g.UUID, g.MapUUID, g.Version, g.Name, g.ExternalID, g.Latitude, g.Longitude, g.Street, g.HouseNumber, g.Postcode, g.City, g.CityDistrict, g.CreatedAt, g.UpdatedAt, g.CreatedBy, g.UpdatedBy).
		Scan(&g.UUID, &g.MapUUID, &g.Version, &g.Name, &g.ExternalID, &g.Latitude, &g.Longitude, &g.Street, &g.HouseNumber, &g.Postcode, &g.City, &g.CityDistrict, &g.CreatedAt, &g.UpdatedAt, &g.CreatedBy, &g.UpdatedBy)
	if err != nil {
		if isPgErrCode(err, "23503") {
			return GeoObjectRecord{}, ErrGeoObjectInvalid
		}

		return GeoObjectRecord{}, fmt.Errorf("upsert synced geo object: %w", err)
	}

	return g, nil
}
