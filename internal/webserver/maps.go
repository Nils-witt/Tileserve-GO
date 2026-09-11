package webserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"nilswitt.dev/tileserve-go/internal/auth"
	"nilswitt.dev/tileserve-go/internal/httputil"
	"nilswitt.dev/tileserve-go/internal/webserver/auditlog"

	"nilswitt.dev/tileserve-go/internal/store"
	"nilswitt.dev/tileserve-go/internal/tilearchive"
)

// currentVersionKeyword, used in place of a literal version segment,
// resolves to the map's MapRecord.CurrentVersion.
const currentVersionKeyword = "current"

type mapRequest struct {
	Name             string `json:"name"`
	CurrentVersion   string `json:"currentVersion"`
	VisibleToAll     bool   `json:"visibleToAll"`
	AnonymousAllowed bool   `json:"anonymousAllowed"`
}

// writeStoreError maps a Store error to an HTTP response: sentinel maps to
// sentinelStatus with sentinelMsg (e.g. a not-found or validation error),
// any other non-nil error maps to a 500 with failMsg.
func writeStoreError(w http.ResponseWriter, err, sentinel error, sentinelStatus int, sentinelMsg, failMsg string) {
	if errors.Is(err, sentinel) {
		http.Error(w, sentinelMsg, sentinelStatus)
		return
	}

	http.Error(w, failMsg, http.StatusInternalServerError)
}

// apiKeyMapAllowed reports whether the API key (if any) authenticating this
// request may access mapID at all. A request authenticated by a human login
// token (no API key in context) always passes — scoping only ever restricts
// an API key's own access, never a user's login session.
func apiKeyMapAllowed(ctx context.Context, st *store.Store, mapID uuid.UUID) (bool, error) {
	apiKeyID, ok := auth.APIKeyIDFromContext(ctx)
	if !ok {
		return true, nil
	}

	return st.APIKeyCanAccessMap(ctx, apiKeyID, mapID)
}

// apiKeyMapVersionAllowed reports whether the API key (if any) authenticating
// this request may access version of mapID. See apiKeyMapAllowed.
func apiKeyMapVersionAllowed(ctx context.Context, st *store.Store, mapID uuid.UUID, version string) (bool, error) {
	apiKeyID, ok := auth.APIKeyIDFromContext(ctx)
	if !ok {
		return true, nil
	}

	return st.APIKeyCanAccessMapVersion(ctx, apiKeyID, mapID, version)
}

// isMapOwner reports whether username is mapID's owner. An owner can do
// everything with their own map, regardless of global or per-map grants (see
// requireMapPermission/requireMapAdmin). A map's owner starts out as its
// creator but can be transferred to someone else later (see
// updateMapOwnerItem), so this is not always the same as who created it. A
// nonexistent map reports false rather than an error, matching
// requireMapPermission's existing behavior of deferring the not-found case
// to the underlying store operation.
func isMapOwner(ctx context.Context, st *store.Store, mapID uuid.UUID, username string) (bool, error) {
	m, err := st.GetMap(ctx, mapID)
	if errors.Is(err, store.ErrMapNotFound) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return m.Owner == username, nil
}

// requireMapPermission checks whether the acting user may perform an action
// on a specific map: it passes if their global permissions allow it (admins
// always pass), if they own the map (who can do everything with it), or
// failing that, if they hold a matching per-map grant. A per-map
// grant only ever adds capability on top of the global flags, never removes
// it. Regardless of that outcome, an API-key-authenticated request
// additionally requires mapID to be within the key's scope (see
// apiKeyMapAllowed) — scope only ever narrows what the request could
// otherwise do.
func requireMapPermission(w http.ResponseWriter, r *http.Request, st *store.Store, mapID uuid.UUID, globalAllowed func(store.Permissions) bool, mapAllowed func(store.MapPermission) bool) bool {
	perms, ok := GetPermissionsOrFail(w, r, st)
	if !ok {
		return false
	}

	if !perms.IsAdmin && !globalAllowed(perms) {
		allowed, err := ownerOrMapAllowed(r.Context(), st, mapID, auth.UsernameFromContext(r.Context()), mapAllowed)
		if err != nil {
			http.Error(w, "failed to check permissions", http.StatusInternalServerError)
			return false
		}

		if !allowed {
			http.Error(w, "forbidden", http.StatusForbidden)
			return false
		}
	}

	allowed, err := apiKeyMapAllowed(r.Context(), st, mapID)
	if err != nil {
		http.Error(w, "failed to check permissions", http.StatusInternalServerError)
		return false
	}

	if !allowed {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}

	return true
}

// ownerOrMapAllowed reports whether username may proceed on mapID because
// they own it (see isMapOwner) or, failing that, because their per-map grant
// satisfies mapAllowed.
func ownerOrMapAllowed(ctx context.Context, st *store.Store, mapID uuid.UUID, username string, mapAllowed func(store.MapPermission) bool) (bool, error) {
	owner, err := isMapOwner(ctx, st, mapID, username)
	if err != nil {
		return false, err
	}

	if owner {
		return true, nil
	}

	mp, err := st.GetMapPermission(ctx, mapID, username)
	if err != nil {
		return false, err
	}

	return mapAllowed(mp), nil
}

// requireMapAdmin checks whether the acting user may administer a specific
// map: global admins always pass, and so does the map's owner — managing who
// else can access their own map (and transferring ownership itself, see
// updateMapOwnerItem) is part of an owner being able to do everything with
// it. Anyone else gets a 403, same as requireMapPermission, without
// revealing whether mapID exists.
func requireMapAdmin(w http.ResponseWriter, r *http.Request, st *store.Store, mapID uuid.UUID) bool {
	perms, ok := GetPermissionsOrFail(w, r, st)
	if !ok {
		return false
	}

	if perms.IsAdmin {
		return true
	}

	owner, err := isMapOwner(r.Context(), st, mapID, auth.UsernameFromContext(r.Context()))
	if err != nil {
		http.Error(w, "failed to check permissions", http.StatusInternalServerError)
		return false
	}

	if owner {
		return true
	}

	http.Error(w, "forbidden", http.StatusForbidden)

	return false
}

// canViewMap reports whether username may see m. Maps are private by
// default: a user can see one because it's marked visible to everyone,
// because they own it, because they're an admin or already hold a
// global permission letting them modify any map (can_edit/can_delete —
// hiding a map from someone who can already act on it would be
// nonsensical), because they hold can_view_all (a standalone permission
// granting blanket visibility without any modify capability), or because
// they hold a per-map grant (view, edit, or delete — any of the three
// implies visibility). Regardless of that outcome, an API-key-authenticated
// request additionally requires m to be within the key's scope (see
// apiKeyMapAllowed).
func canViewMap(ctx context.Context, st *store.Store, m store.MapRecord, username string) (bool, error) {
	visible, err := userCanViewMap(ctx, st, m, username)
	if err != nil || !visible {
		return false, err
	}

	return apiKeyMapAllowed(ctx, st, m.UUID)
}

// userCanViewMap is canViewMap's check of the acting user's own
// visibility, without regard to any API-key scope restriction.
func userCanViewMap(ctx context.Context, st *store.Store, m store.MapRecord, username string) (bool, error) {
	if m.VisibleToAll || m.Owner == username {
		return true, nil
	}

	perms, err := st.GetPermissions(ctx, username)
	if err != nil {
		return false, err
	}

	if perms.GrantsMapVisibility() {
		return true, nil
	}

	mp, err := st.GetMapPermission(ctx, m.UUID, username)
	if err != nil {
		return false, err
	}

	return mp.GrantsVisibility(), nil
}

// requireMapView checks canViewMap and writes a 403 if it fails. It returns
// true when the caller may continue.
func requireMapView(w http.ResponseWriter, r *http.Request, st *store.Store, m store.MapRecord) bool {
	ok, err := canViewMap(r.Context(), st, m, auth.UsernameFromContext(r.Context()))
	if err != nil {
		http.Error(w, "failed to check permissions", http.StatusInternalServerError)
		return false
	}

	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}

	return true
}

// getViewableMap fetches id and checks the acting user may view it, writing
// the appropriate error response (404/403/500) if not. ok is false when the
// caller should stop.
func getViewableMap(w http.ResponseWriter, r *http.Request, st *store.Store, id uuid.UUID) (m store.MapRecord, ok bool) {
	m, err := st.GetMap(r.Context(), id)
	if err != nil {
		writeStoreError(w, err, store.ErrMapNotFound, http.StatusNotFound, "map not found", "failed to get map")
		return store.MapRecord{}, false
	}

	if !requireMapView(w, r, st, m) {
		return store.MapRecord{}, false
	}

	return m, true
}

// ServeMapVersionFileHandler serves a single extracted tile file from a map
// version's directory. It's the one route reachable without a bearer token,
// when the map itself opts in via anonymousAllowed. Registered twice in
// main.go: once for the exact "/maps/{id}/version/{version}" pattern (no
// trailing slash, no file — falls through to a 404 via http.StripPrefix's
// prefix-mismatch) and once for the "/maps/{id}/version/{version}/{filepath...}"
// wildcard that serves the actual tile files. Both point at this same
// handler.
func ServeMapVersionFileHandler(st *store.Store, dataRoot string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httputil.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		m, err := st.GetMap(r.Context(), id)
		if err != nil {
			writeStoreError(w, err, store.ErrMapNotFound, http.StatusNotFound, "map not found", "failed to get map")
			return
		}

		if !m.AnonymousAllowed {
			if !RequireAuthenticated(w, r) || !requireMapView(w, r, st, m) {
				return
			}
		}

		rawVersion := r.PathValue("version")

		version, ok := resolveVersionSegment(w, r, st, id, rawVersion)
		if !ok {
			return
		}

		versionDir := tilearchive.MapVersionDir(dataRoot, id, version)
		prefix := "/maps/" + r.PathValue("id") + "/version/" + rawVersion + "/"

		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.StripPrefix(prefix, http.FileServer(http.Dir(versionDir))).ServeHTTP(w, r)
	}
}

// resolveCurrentVersion fetches m for id and returns its current version,
// writing a 404 and returning ok=false if the map doesn't exist or has no
// current version yet (an empty CurrentVersion must never be used as a path
// segment — tilearchive.MapVersionDir would silently resolve to the map's root
// directory, exposing every version).
func resolveCurrentVersion(w http.ResponseWriter, r *http.Request, st *store.Store, id uuid.UUID) (version string, ok bool) {
	version, err := st.GetCurrentVersion(r.Context(), id)
	if err != nil {
		writeStoreError(w, err, store.ErrMapNotFound, http.StatusNotFound, "map not found", "failed to get map")
		return "", false
	}

	if version == "" {
		http.Error(w, "map has no current version", http.StatusNotFound)
		return "", false
	}

	return version, true
}

// versionSegmentKind classifies a {version} path segment into the branch of
// resolveVersionSegment that handles it.
type versionSegmentKind int

const (
	versionSegmentCurrent versionSegmentKind = iota
	versionSegmentNumeric
	versionSegmentAlias
)

// classifyVersionSegment reports which resolution branch segment falls
// into, without performing any I/O. Real version identifiers are always the
// string form of an incrementing integer (see IncrementMapVersion), so any
// non-numeric, non-"current" segment is unambiguously an alias candidate.
func classifyVersionSegment(segment string) versionSegmentKind {
	switch {
	case segment == currentVersionKeyword:
		return versionSegmentCurrent
	case tilearchive.NumericSegmentRE.MatchString(segment):
		return versionSegmentNumeric
	default:
		return versionSegmentAlias
	}
}

// resolveVersionSegment resolves a {version} path segment to an actual
// version identifier: the literal keyword "current" resolves via
// resolveCurrentVersion, a purely numeric segment is a real version
// identifier and is used as-is, and anything else is looked up as a
// user-defined alias for id. It writes the appropriate error response (404
// for an unknown map/alias/missing current version, 500 on lookup failure)
// and returns ok=false if resolution fails; the caller must stop handling
// the request in that case. Once resolved, an API-key-authenticated request
// additionally requires the resulting version to be within the key's scope
// for id (see apiKeyMapVersionAllowed) — this is the single chokepoint
// behind every version-scoped route (tile file serving, bounds, archive,
// geo-objects), so version-level scope restriction is enforced here once.
func resolveVersionSegment(w http.ResponseWriter, r *http.Request, st *store.Store, id uuid.UUID, segment string) (version string, ok bool) {
	switch classifyVersionSegment(segment) {
	case versionSegmentCurrent:
		version, ok = resolveCurrentVersion(w, r, st, id)

	case versionSegmentNumeric:
		version, ok = segment, true

	case versionSegmentAlias:
		resolved, err := st.GetMapVersionAlias(r.Context(), id, segment)
		if err != nil {
			writeStoreError(w, err, store.ErrMapVersionAliasNotFound, http.StatusNotFound, "alias not found", "failed to resolve alias")
			return "", false
		}

		version, ok = resolved, true
	}

	if !ok {
		return "", false
	}

	allowed, err := apiKeyMapVersionAllowed(r.Context(), st, id, version)
	if err != nil {
		http.Error(w, "failed to check permissions", http.StatusInternalServerError)
		return "", false
	}

	if !allowed {
		http.Error(w, "forbidden", http.StatusForbidden)
		return "", false
	}

	return version, true
}

// MapsListHandler serves GET /maps: lists the maps visible to the caller,
// honoring both stored visibility rules and any API-key scope restriction.
func MapsListHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := auth.UsernameFromContext(r.Context())

		perms, ok := GetPermissionsOrFail(w, r, st)
		if !ok {
			return
		}

		bypassVisibility := perms.GrantsMapVisibility()

		visibleToAll, ok := httputil.QueryBoolParam(w, r, "visibleToAll")
		if !ok {
			return
		}

		anonymousAllowed, ok := httputil.QueryBoolParam(w, r, "anonymousAllowed")
		if !ok {
			return
		}

		filter := store.MapFilter{
			Name:             r.URL.Query().Get("name"),
			CreatedBy:        r.URL.Query().Get("createdBy"),
			VisibleToAll:     visibleToAll,
			AnonymousAllowed: anonymousAllowed,
		}

		maps, err := st.ListMaps(r.Context(), username, bypassVisibility, filter)
		if err != nil {
			http.Error(w, "failed to list maps", http.StatusInternalServerError)
			return
		}

		maps, err = filterMapsByAPIKeyScope(r.Context(), st, maps)
		if err != nil {
			http.Error(w, "failed to list maps", http.StatusInternalServerError)
			return
		}

		httputil.WriteJSON(w, http.StatusOK, maps)
	}
}

// filterMapsByAPIKeyScope narrows maps to those the request's API key (if
// any) may access; a request authenticated by a human login token (no API
// key in context) always passes all of them through unfiltered.
func filterMapsByAPIKeyScope(ctx context.Context, st *store.Store, maps []store.MapRecord) ([]store.MapRecord, error) {
	apiKeyID, ok := auth.APIKeyIDFromContext(ctx)
	if !ok {
		return maps, nil
	}

	filtered := make([]store.MapRecord, 0, len(maps))

	for _, m := range maps {
		allowed, err := st.APIKeyCanAccessMap(ctx, apiKeyID, m.UUID)
		if err != nil {
			return nil, err
		}

		if allowed {
			filtered = append(filtered, m)
		}
	}

	return filtered, nil
}

// MapCreateHandler serves POST /maps: creates a new map.
func MapCreateHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !RequirePermission(w, r, st, func(p store.Permissions) bool { return p.CanCreate }) {
			return
		}

		var req mapRequest
		if !httputil.DecodeJSON(w, r, &req) {
			return
		}

		if req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}

		m, err := st.CreateMap(r.Context(), req.Name, req.CurrentVersion, req.VisibleToAll, req.AnonymousAllowed, auth.UsernameFromContext(r.Context()))
		if err != nil {
			http.Error(w, "failed to create map", http.StatusInternalServerError)
			return
		}

		auditlog.RecordAudit(r, st, "create", "map", m.UUID.String(), fmt.Sprintf("name=%q visibleToAll=%v anonymousAllowed=%v", m.Name, m.VisibleToAll, m.AnonymousAllowed))

		httputil.WriteJSON(w, http.StatusCreated, m)
	}
}

// GetMapHandler serves GET /maps/{id}: fetch the map itself (requires view
// access).
func GetMapHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httputil.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		m, ok := getViewableMap(w, r, st, id)
		if ok {
			httputil.WriteJSON(w, http.StatusOK, m)
		}
	}
}

// UpdateMapHandler serves PUT /maps/{id}: update the map itself.
func UpdateMapHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httputil.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		updateMapItem(w, r, st, id)
	}
}

// DeleteMapHandler serves DELETE /maps/{id}: delete the map itself.
func DeleteMapHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httputil.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		deleteMapItem(w, r, st, id)
	}
}

func updateMapItem(w http.ResponseWriter, r *http.Request, st *store.Store, id uuid.UUID) {
	if !requireMapPermission(w, r, st, id,
		func(p store.Permissions) bool { return p.CanEdit },
		func(mp store.MapPermission) bool { return mp.CanEdit },
	) {
		return
	}

	var req mapRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	m, err := st.UpdateMap(r.Context(), id, req.Name, req.CurrentVersion, req.VisibleToAll, req.AnonymousAllowed, auth.UsernameFromContext(r.Context()))
	if err != nil {
		writeStoreError(w, err, store.ErrMapNotFound, http.StatusNotFound, "map not found", "failed to update map")
		return
	}

	auditlog.RecordAudit(r, st, "update", "map", m.UUID.String(), fmt.Sprintf("name=%q visibleToAll=%v anonymousAllowed=%v", m.Name, m.VisibleToAll, m.AnonymousAllowed))

	httputil.WriteJSON(w, http.StatusOK, m)
}

func deleteMapItem(w http.ResponseWriter, r *http.Request, st *store.Store, id uuid.UUID) {
	if !requireMapPermission(w, r, st, id,
		func(p store.Permissions) bool { return p.CanDelete },
		func(mp store.MapPermission) bool { return mp.CanDelete },
	) {
		return
	}

	if err := st.DeleteMap(r.Context(), id); err != nil {
		writeStoreError(w, err, store.ErrMapNotFound, http.StatusNotFound, "map not found", "failed to delete map")
		return
	}

	auditlog.RecordAudit(r, st, "delete", "map", id.String(), "")

	w.WriteHeader(http.StatusNoContent)
}

// MapVersionsHandler serves GET /maps/{id}/versions: the upload history for
// a map. If the request is authenticated by a scoped API key that carries a
// version whitelist for id, the result is filtered down to just those
// versions (see store.APIKeyScopedVersions) — getViewableMap has already
// rejected the request outright if id itself isn't within the key's scope.
func MapVersionsHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httputil.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		if _, ok := getViewableMap(w, r, st, id); !ok {
			return
		}

		versions, err := st.ListMapVersions(r.Context(), id)
		if err != nil {
			writeStoreError(w, err, store.ErrMapNotFound, http.StatusNotFound, "map not found", "failed to list map versions")
			return
		}

		if apiKeyID, ok := auth.APIKeyIDFromContext(r.Context()); ok {
			allowed, restricted, err := st.APIKeyScopedVersions(r.Context(), apiKeyID, id)
			if err != nil {
				http.Error(w, "failed to list map versions", http.StatusInternalServerError)
				return
			}

			if restricted {
				versions = slices.DeleteFunc(versions, func(v store.MapVersionRecord) bool {
					return !slices.Contains(allowed, v.Version)
				})
			}
		}

		httputil.WriteJSON(w, http.StatusOK, versions)
	}
}

type mapPermissionRequest struct {
	CanView             bool `json:"canView"`
	CanEdit             bool `json:"canEdit"`
	CanDelete           bool `json:"canDelete"`
	CanEditGeoObjects   bool `json:"canEditGeoObjects"`
	CanDeleteGeoObjects bool `json:"canDeleteGeoObjects"`
}

// MapPermissionsListHandler serves GET /maps/{id}/permissions: a map's
// per-user permission grants. Managing per-map permissions requires either
// the global is_admin permission (same as the Users API) or being the map's
// own owner.
func MapPermissionsListHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httputil.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		if !requireMapAdmin(w, r, st, id) {
			return
		}

		perms, err := st.ListMapPermissions(r.Context(), id)
		if err != nil {
			http.Error(w, "failed to list map permissions", http.StatusInternalServerError)
			return
		}

		httputil.WriteJSON(w, http.StatusOK, perms)
	}
}

// MapPermissionSetHandler serves PUT /maps/{id}/permissions/{username}:
// grants a single user's per-map permission. Managing per-map permissions
// requires either the global is_admin permission (same as the Users API) or
// being the map's own owner.
func MapPermissionSetHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httputil.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		if !requireMapAdmin(w, r, st, id) {
			return
		}

		username := r.PathValue("username")

		var req mapPermissionRequest
		if !httputil.DecodeJSON(w, r, &req) {
			return
		}

		p, err := st.SetMapPermission(r.Context(), id, username, req.CanView, req.CanEdit, req.CanDelete, req.CanEditGeoObjects, req.CanDeleteGeoObjects, auth.UsernameFromContext(r.Context()))
		if err != nil {
			writeStoreError(w, err, store.ErrMapPermissionInvalid, http.StatusBadRequest, "map or username does not exist", "failed to set map permission")
			return
		}

		auditlog.RecordAudit(r, st, "grant", "map_permission", id.String()+":"+username, fmt.Sprintf("view=%v edit=%v delete=%v editGeo=%v deleteGeo=%v", req.CanView, req.CanEdit, req.CanDelete, req.CanEditGeoObjects, req.CanDeleteGeoObjects))

		httputil.WriteJSON(w, http.StatusOK, p)
	}
}

// MapPermissionDeleteHandler serves DELETE /maps/{id}/permissions/{username}:
// revokes a single user's per-map permission. See MapPermissionSetHandler for
// the access rule.
func MapPermissionDeleteHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httputil.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		if !requireMapAdmin(w, r, st, id) {
			return
		}

		username := r.PathValue("username")

		if err := st.DeleteMapPermission(r.Context(), id, username); err != nil {
			http.Error(w, "failed to delete map permission", http.StatusInternalServerError)
			return
		}

		auditlog.RecordAudit(r, st, "revoke", "map_permission", id.String()+":"+username, "")

		w.WriteHeader(http.StatusNoContent)
	}
}

type groupMapPermissionRequest struct {
	CanView             bool `json:"canView"`
	CanEdit             bool `json:"canEdit"`
	CanDelete           bool `json:"canDelete"`
	CanEditGeoObjects   bool `json:"canEditGeoObjects"`
	CanDeleteGeoObjects bool `json:"canDeleteGeoObjects"`
}

// GroupMapPermissionsListHandler serves GET /maps/{id}/group-permissions: a
// map's per-group permission grants. See MapPermissionsListHandler for the
// access rule.
func GroupMapPermissionsListHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httputil.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		if !requireMapAdmin(w, r, st, id) {
			return
		}

		perms, err := st.ListGroupMapPermissions(r.Context(), id)
		if err != nil {
			http.Error(w, "failed to list group map permissions", http.StatusInternalServerError)
			return
		}

		httputil.WriteJSON(w, http.StatusOK, perms)
	}
}

// GroupMapPermissionSetHandler serves PUT
// /maps/{id}/group-permissions/{groupId}: grants a single group's per-map
// permission, inherited by every current member of that group. See
// MapPermissionSetHandler for the access rule.
func GroupMapPermissionSetHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httputil.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		if !requireMapAdmin(w, r, st, id) {
			return
		}

		groupID, ok := httputil.PathUUID(w, r, "groupId", "group id")
		if !ok {
			return
		}

		var req groupMapPermissionRequest
		if !httputil.DecodeJSON(w, r, &req) {
			return
		}

		p, err := st.SetGroupMapPermission(r.Context(), id, groupID, req.CanView, req.CanEdit, req.CanDelete, req.CanEditGeoObjects, req.CanDeleteGeoObjects, auth.UsernameFromContext(r.Context()))
		if err != nil {
			writeStoreError(w, err, store.ErrMapPermissionInvalid, http.StatusBadRequest, "map or group does not exist", "failed to set group map permission")
			return
		}

		auditlog.RecordAudit(r, st, "grant", "group_map_permission", id.String()+":"+groupID.String(), fmt.Sprintf("view=%v edit=%v delete=%v editGeo=%v deleteGeo=%v", req.CanView, req.CanEdit, req.CanDelete, req.CanEditGeoObjects, req.CanDeleteGeoObjects))

		httputil.WriteJSON(w, http.StatusOK, p)
	}
}

// GroupMapPermissionDeleteHandler serves DELETE
// /maps/{id}/group-permissions/{groupId}: revokes a single group's per-map
// permission. See MapPermissionSetHandler for the access rule.
func GroupMapPermissionDeleteHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httputil.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		if !requireMapAdmin(w, r, st, id) {
			return
		}

		groupID, ok := httputil.PathUUID(w, r, "groupId", "group id")
		if !ok {
			return
		}

		if err := st.DeleteGroupMapPermission(r.Context(), id, groupID); err != nil {
			http.Error(w, "failed to delete group map permission", http.StatusInternalServerError)
			return
		}

		auditlog.RecordAudit(r, st, "revoke", "group_map_permission", id.String()+":"+groupID.String(), "")

		w.WriteHeader(http.StatusNoContent)
	}
}

type mapOwnerRequest struct {
	Owner string `json:"owner"`
}

// MapOwnerGetHandler serves GET /maps/{id}/owner: fetches a map's owner
// (requires view access).
func MapOwnerGetHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httputil.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		m, ok := getViewableMap(w, r, st, id)
		if !ok {
			return
		}

		httputil.WriteJSON(w, http.StatusOK, mapOwnerRequest{Owner: m.Owner})
	}
}

// MapOwnerSetHandler serves PUT /maps/{id}/owner: transfers a map's owner.
// Transferring ownership requires the same access as managing the map's
// permission grants (requireMapAdmin: global admin or the map's current
// owner) — an owner able to do everything with their own map, including
// handing it off to someone else.
func MapOwnerSetHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httputil.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		if !requireMapAdmin(w, r, st, id) {
			return
		}

		var req mapOwnerRequest
		if !httputil.DecodeJSON(w, r, &req) {
			return
		}

		if req.Owner == "" {
			http.Error(w, "owner is required", http.StatusBadRequest)
			return
		}

		m, err := st.UpdateMapOwner(r.Context(), id, req.Owner)
		if errors.Is(err, store.ErrMapNotFound) {
			http.Error(w, "map not found", http.StatusNotFound)
			return
		}

		if err != nil {
			writeStoreError(w, err, store.ErrUserNotFound, http.StatusBadRequest, "owner does not exist", "failed to update map owner")
			return
		}

		auditlog.RecordAudit(r, st, "update", "map_owner", id.String(), "owner="+req.Owner)

		httputil.WriteJSON(w, http.StatusOK, mapOwnerRequest{Owner: m.Owner})
	}
}
