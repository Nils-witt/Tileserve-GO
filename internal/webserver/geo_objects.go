package webserver

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"nilswitt.dev/tileserve-go/internal/auth"
	"nilswitt.dev/tileserve-go/internal/httputil"
	"nilswitt.dev/tileserve-go/internal/webserver/auditlog"

	"nilswitt.dev/tileserve-go/internal/store"
)

type geoObjectRequest struct {
	Name         string   `json:"name"`
	ExternalID   string   `json:"externalId"`
	Latitude     *float64 `json:"latitude"`
	Longitude    *float64 `json:"longitude"`
	Street       string   `json:"street"`
	HouseNumber  string   `json:"housenumber"`
	Postcode     string   `json:"postcode"`
	City         string   `json:"city"`
	CityDistrict string   `json:"cityDistrict"`
}

// decodeGeoObjectRequest decodes and validates req's body, writing a 400
// response and returning ok=false if the name is empty or latitude/longitude
// are missing. Latitude/longitude are pointers specifically so an omitted
// value (nil) can be distinguished from an explicit 0.0 (a valid coordinate).
func decodeGeoObjectRequest(w http.ResponseWriter, r *http.Request) (req geoObjectRequest, ok bool) {
	if !httputil.DecodeJSON(w, r, &req) {
		return geoObjectRequest{}, false
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return geoObjectRequest{}, false
	}

	if req.Latitude == nil || req.Longitude == nil {
		http.Error(w, "latitude and longitude are required", http.StatusBadRequest)
		return geoObjectRequest{}, false
	}

	return req, true
}

// geoObjectFilterFromQuery builds a store.GeoObjectFilter from r's query
// parameters, writing a 400 response and returning ok=false if a numeric
// bbox parameter is malformed, or if only some of minLat/maxLat/minLon/
// maxLon are given (they scope one bounding box and must be given together).
func geoObjectFilterFromQuery(w http.ResponseWriter, r *http.Request) (filter store.GeoObjectFilter, ok bool) {
	minLat, ok := httputil.QueryFloatParam(w, r, "minLat")
	if !ok {
		return store.GeoObjectFilter{}, false
	}

	maxLat, ok := httputil.QueryFloatParam(w, r, "maxLat")
	if !ok {
		return store.GeoObjectFilter{}, false
	}

	minLon, ok := httputil.QueryFloatParam(w, r, "minLon")
	if !ok {
		return store.GeoObjectFilter{}, false
	}

	maxLon, ok := httputil.QueryFloatParam(w, r, "maxLon")
	if !ok {
		return store.GeoObjectFilter{}, false
	}

	given := 0

	for _, v := range []*float64{minLat, maxLat, minLon, maxLon} {
		if v != nil {
			given++
		}
	}

	if given != 0 && given != 4 {
		http.Error(w, "minLat, maxLat, minLon, and maxLon must be given together", http.StatusBadRequest)
		return store.GeoObjectFilter{}, false
	}

	return store.GeoObjectFilter{
		Name:         r.URL.Query().Get("name"),
		ExternalID:   r.URL.Query().Get("externalId"),
		Street:       r.URL.Query().Get("street"),
		Postcode:     r.URL.Query().Get("postcode"),
		City:         r.URL.Query().Get("city"),
		CityDistrict: r.URL.Query().Get("cityDistrict"),
		CreatedBy:    r.URL.Query().Get("createdBy"),
		MinLat:       minLat,
		MaxLat:       maxLat,
		MinLon:       minLon,
		MaxLon:       maxLon,
	}, true
}

// GeoObjectsListHandler serves GET /maps/{id}/version/{version}/geo-objects:
// lists the geo objects tied to this map version (requires view access to
// the map).
func GeoObjectsListHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mapID, ok := httputil.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		if _, ok := getViewableMap(w, r, st, mapID); !ok {
			return
		}

		version, ok := resolveVersionSegment(w, r, st, mapID, r.PathValue("version"))
		if !ok {
			return
		}

		filter, ok := geoObjectFilterFromQuery(w, r)
		if !ok {
			return
		}

		objs, err := st.ListGeoObjects(r.Context(), mapID, version, filter)
		if err != nil {
			http.Error(w, "failed to list geo objects", http.StatusInternalServerError)
			return
		}

		httputil.WriteJSON(w, http.StatusOK, objs)
	}
}

// GeoObjectCreateHandler serves POST /maps/{id}/version/{version}/geo-objects:
// creates a new geo object (requires the can_edit_geo_objects permission,
// global or per-map — a permission separate from the map's own can_edit, so
// it can be granted without also granting the ability to edit the map
// itself. It implies view access, so no separate check is needed).
func GeoObjectCreateHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mapID, ok := httputil.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		if !requireMapPermission(w, r, st, mapID,
			func(p store.Permissions) bool { return p.CanEditGeoObjects },
			func(mp store.MapPermission) bool { return mp.CanEditGeoObjects },
		) {
			return
		}

		version, ok := resolveVersionSegment(w, r, st, mapID, r.PathValue("version"))
		if !ok {
			return
		}

		req, ok := decodeGeoObjectRequest(w, r)
		if !ok {
			return
		}

		g, err := st.CreateGeoObject(r.Context(), mapID, version, req.Name, req.ExternalID, *req.Latitude, *req.Longitude, req.Street, req.HouseNumber, req.Postcode, req.City, req.CityDistrict, auth.UsernameFromContext(r.Context()))
		if err != nil {
			writeStoreError(w, err, store.ErrGeoObjectInvalid, http.StatusBadRequest, "map or version does not exist", "failed to create geo object")
			return
		}

		auditlog.RecordAudit(r, st, "create", "geo_object", g.UUID.String(), fmt.Sprintf("map=%s version=%s name=%q", mapID, version, g.Name))

		httputil.WriteJSON(w, http.StatusCreated, g)
	}
}

// getScopedGeoObject fetches id and checks it actually belongs to mapID's
// version, writing a 404 if it doesn't (whether because the id is unknown or
// because it belongs to a different map/version). This keeps the URL's
// map/version scoping honest even though uuid alone is geo_objects' real
// primary key. Only used by the read path; PUT/DELETE scope their single
// write query directly (see UpdateGeoObject/DeleteGeoObject).
func getScopedGeoObject(w http.ResponseWriter, r *http.Request, st *store.Store, mapID uuid.UUID, version string, id uuid.UUID) (g store.GeoObjectRecord, ok bool) {
	g, err := st.GetGeoObject(r.Context(), id)
	if err != nil {
		writeStoreError(w, err, store.ErrGeoObjectNotFound, http.StatusNotFound, "geo object not found", "failed to get geo object")
		return store.GeoObjectRecord{}, false
	}

	if g.MapUUID != mapID || g.Version != version {
		http.Error(w, "geo object not found", http.StatusNotFound)
		return store.GeoObjectRecord{}, false
	}

	return g, true
}

// GeoObjectGetHandler serves GET
// /maps/{id}/version/{version}/geo-objects/{objectId}: fetches a geo object
// (requires view access to the map).
func GeoObjectGetHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mapID, ok := httputil.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		if _, ok := getViewableMap(w, r, st, mapID); !ok {
			return
		}

		version, ok := resolveVersionSegment(w, r, st, mapID, r.PathValue("version"))
		if !ok {
			return
		}

		id, ok := httputil.PathUUID(w, r, "objectId", "geo object id")
		if !ok {
			return
		}

		g, ok := getScopedGeoObject(w, r, st, mapID, version, id)
		if ok {
			httputil.WriteJSON(w, http.StatusOK, g)
		}
	}
}

// GeoObjectUpdateHandler serves PUT
// /maps/{id}/version/{version}/geo-objects/{objectId}: replaces a geo
// object's fields (requires can_edit_geo_objects — separate from the map's
// own can_edit, see GeoObjectCreateHandler, and implies view access, so no
// separate check is needed).
func GeoObjectUpdateHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mapID, ok := httputil.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		if !requireMapPermission(w, r, st, mapID,
			func(p store.Permissions) bool { return p.CanEditGeoObjects },
			func(mp store.MapPermission) bool { return mp.CanEditGeoObjects },
		) {
			return
		}

		version, ok := resolveVersionSegment(w, r, st, mapID, r.PathValue("version"))
		if !ok {
			return
		}

		id, ok := httputil.PathUUID(w, r, "objectId", "geo object id")
		if !ok {
			return
		}

		req, ok := decodeGeoObjectRequest(w, r)
		if !ok {
			return
		}

		g, err := st.UpdateGeoObject(r.Context(), mapID, version, id, req.Name, req.ExternalID, *req.Latitude, *req.Longitude, req.Street, req.HouseNumber, req.Postcode, req.City, req.CityDistrict, auth.UsernameFromContext(r.Context()))
		if err != nil {
			writeStoreError(w, err, store.ErrGeoObjectNotFound, http.StatusNotFound, "geo object not found", "failed to update geo object")
			return
		}

		auditlog.RecordAudit(r, st, "update", "geo_object", g.UUID.String(), fmt.Sprintf("map=%s version=%s name=%q", mapID, version, g.Name))

		httputil.WriteJSON(w, http.StatusOK, g)
	}
}

// GeoObjectDeleteHandler serves DELETE
// /maps/{id}/version/{version}/geo-objects/{objectId}: removes a geo object
// (requires can_delete_geo_objects — separate from the map's own
// can_delete, see GeoObjectCreateHandler, and implies view access, so no
// separate check is needed).
func GeoObjectDeleteHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mapID, ok := httputil.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		if !requireMapPermission(w, r, st, mapID,
			func(p store.Permissions) bool { return p.CanDeleteGeoObjects },
			func(mp store.MapPermission) bool { return mp.CanDeleteGeoObjects },
		) {
			return
		}

		version, ok := resolveVersionSegment(w, r, st, mapID, r.PathValue("version"))
		if !ok {
			return
		}

		id, ok := httputil.PathUUID(w, r, "objectId", "geo object id")
		if !ok {
			return
		}

		if err := st.DeleteGeoObject(r.Context(), mapID, version, id); err != nil {
			writeStoreError(w, err, store.ErrGeoObjectNotFound, http.StatusNotFound, "geo object not found", "failed to delete geo object")
			return
		}

		auditlog.RecordAudit(r, st, "delete", "geo_object", id.String(), fmt.Sprintf("map=%s version=%s", mapID, version))

		w.WriteHeader(http.StatusNoContent)
	}
}
