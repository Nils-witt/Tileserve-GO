package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"

	"nilswitt.dev/tileserve-go/internal/store"
	"nilswitt.dev/tileserve-go/internal/tilearchive"
)

// Path segments used when routing /maps/{id}/... requests in MapsItemHandler.
const (
	versionPathSegment    = "version"
	boundsPathSegment     = "bounds"
	geoObjectsPathSegment = "geo-objects"
	archivePathSegment    = "archive"
	// currentVersionKeyword, used in place of a literal version segment,
	// resolves to the map's MapRecord.CurrentVersion.
	currentVersionKeyword = "current"
)

// isVersionSubResourcePath reports whether segments (the /maps/{id}/...
// path, split on "/") addresses one of the JSON/binary sub-resources nested
// under a map version — .../version/{version}/bounds,
// .../version/{version}/geo-objects[/{uuid}], or
// .../version/{version}/archive — rather than a raw extracted tile file.
// Reserving these path segments is safe: uploaded tile entries are validated
// at extraction time to be purely numeric directories or <number>.png
// files, so a real extracted path can never start with "bounds",
// "geo-objects", or "archive".
func isVersionSubResourcePath(segments []string) bool {
	return len(segments) >= 4 && segments[1] == versionPathSegment &&
		(segments[3] == boundsPathSegment || segments[3] == geoObjectsPathSegment || segments[3] == archivePathSegment)
}

type mapRequest struct {
	Name             string `json:"name"`
	CurrentVersion   string `json:"currentVersion"`
	VisibleToAll     bool   `json:"visibleToAll"`
	AnonymousAllowed bool   `json:"anonymousAllowed"`
}

// writeJSON writes v as a JSON response body with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// decodeJSON decodes r's body as JSON into v, writing a 400 response and
// returning false if the body isn't valid JSON.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}

	return true
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

// requireMethod rejects a request whose method isn't method with a 405. It
// returns true when the caller may continue.
func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}

	return true
}

// getPermissionsOrFail fetches the acting user's global permissions, writing
// a 500 response and returning ok=false if that fails.
func getPermissionsOrFail(w http.ResponseWriter, r *http.Request, st *store.Store) (perms store.Permissions, ok bool) {
	perms, err := st.GetPermissions(r.Context(), usernameFromContext(r.Context()))
	if err != nil {
		http.Error(w, "failed to check permissions", http.StatusInternalServerError)
		return store.Permissions{}, false
	}

	return perms, true
}

// requireAuthenticated rejects a request with no bearer token. /maps/ is
// mounted behind OptionalAuth (see cmd/tileserve-go/main.go) so that a
// map's version file serving route can allow anonymous requests when that
// map opts in via anonymousAllowed; every other route under /maps/ calls
// this to restore the usual "must be logged in" requirement.
func requireAuthenticated(w http.ResponseWriter, r *http.Request) bool {
	if usernameFromContext(r.Context()) == "" {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return false
	}

	return true
}

// requirePermission checks the acting user's global permissions and writes an
// error response if the request should not proceed. It returns true when the
// caller may continue.
func requirePermission(w http.ResponseWriter, r *http.Request, st *store.Store, allowed func(store.Permissions) bool) bool {
	perms, ok := getPermissionsOrFail(w, r, st)
	if !ok {
		return false
	}

	if !allowed(perms) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}

	return true
}

// apiKeyMapAllowed reports whether the API key (if any) authenticating this
// request may access mapID at all. A request authenticated by a human login
// token (no API key in context) always passes — scoping only ever restricts
// an API key's own access, never a user's login session.
func apiKeyMapAllowed(ctx context.Context, st *store.Store, mapID uuid.UUID) (bool, error) {
	apiKeyID, ok := apiKeyIDFromContext(ctx)
	if !ok {
		return true, nil
	}

	return st.APIKeyCanAccessMap(ctx, apiKeyID, mapID)
}

// apiKeyMapVersionAllowed reports whether the API key (if any) authenticating
// this request may access version of mapID. See apiKeyMapAllowed.
func apiKeyMapVersionAllowed(ctx context.Context, st *store.Store, mapID uuid.UUID, version string) (bool, error) {
	apiKeyID, ok := apiKeyIDFromContext(ctx)
	if !ok {
		return true, nil
	}

	return st.APIKeyCanAccessMapVersion(ctx, apiKeyID, mapID, version)
}

// filterMapsByAPIKeyScope drops any map outside the scope of the API key (if
// any) authenticating this request, preserving order. It's a no-op — maps
// returned unchanged — for a request with no API key in context.
func filterMapsByAPIKeyScope(ctx context.Context, st *store.Store, maps []store.MapRecord) ([]store.MapRecord, error) {
	apiKeyID, ok := apiKeyIDFromContext(ctx)
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

// requireMapPermission checks whether the acting user may perform an action
// on a specific map: it passes if their global permissions allow it (admins
// always pass), or failing that, if they hold a matching per-map grant. A
// per-map grant only ever adds capability on top of the global flags, never
// removes it. Regardless of that outcome, an API-key-authenticated request
// additionally requires mapID to be within the key's scope (see
// apiKeyMapAllowed) — scope only ever narrows what the request could
// otherwise do.
func requireMapPermission(w http.ResponseWriter, r *http.Request, st *store.Store, mapID uuid.UUID, globalAllowed func(store.Permissions) bool, mapAllowed func(store.MapPermission) bool) bool {
	username := usernameFromContext(r.Context())

	perms, ok := getPermissionsOrFail(w, r, st)
	if !ok {
		return false
	}

	if !perms.IsAdmin && !globalAllowed(perms) {
		mp, err := st.GetMapPermission(r.Context(), mapID, username)
		if err != nil {
			http.Error(w, "failed to check permissions", http.StatusInternalServerError)
			return false
		}

		if !mapAllowed(mp) {
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

// canViewMap reports whether username may see m. Maps are private by
// default: a user can see one because it's marked visible to everyone,
// because they created it, because they're an admin or already hold a
// global permission letting them modify any map (can_edit/can_delete —
// hiding a map from someone who can already act on it would be
// nonsensical), or because they hold a per-map grant (view, edit, or
// delete — any of the three implies visibility). Regardless of that
// outcome, an API-key-authenticated request additionally requires m to be
// within the key's scope (see apiKeyMapAllowed).
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
	if m.VisibleToAll || m.CreatedBy == username {
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
	ok, err := canViewMap(r.Context(), st, m, usernameFromContext(r.Context()))
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

// listMaps serves the GET branch of the /maps collection route: it lists the
// maps visible to the caller, filtered further by query params, then by the
// scope of the API key (if any) authenticating the request (see
// filterMapsByAPIKeyScope).
func listMaps(w http.ResponseWriter, r *http.Request, st *store.Store) {
	username := usernameFromContext(r.Context())

	perms, ok := getPermissionsOrFail(w, r, st)
	if !ok {
		return
	}

	bypassVisibility := perms.GrantsMapVisibility()

	visibleToAll, ok := queryBoolParam(w, r, "visibleToAll")
	if !ok {
		return
	}

	anonymousAllowed, ok := queryBoolParam(w, r, "anonymousAllowed")
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

	writeJSON(w, http.StatusOK, maps)
}

// MapsCollectionHandler serves the /maps collection route: GET lists the
// maps visible to the caller, POST creates a new map (requires the
// can_create global permission).
func MapsCollectionHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			listMaps(w, r, st)

		case http.MethodPost:
			if !requirePermission(w, r, st, func(p store.Permissions) bool { return p.CanCreate }) {
				return
			}

			var req mapRequest
			if !decodeJSON(w, r, &req) {
				return
			}

			if req.Name == "" {
				http.Error(w, "name is required", http.StatusBadRequest)
				return
			}

			m, err := st.CreateMap(r.Context(), req.Name, req.CurrentVersion, req.VisibleToAll, req.AnonymousAllowed, usernameFromContext(r.Context()))
			if err != nil {
				http.Error(w, "failed to create map", http.StatusInternalServerError)
				return
			}

			recordAudit(r, st, "create", "map", m.UUID.String(), fmt.Sprintf("name=%q visibleToAll=%v anonymousAllowed=%v", m.Name, m.VisibleToAll, m.AnonymousAllowed))

			writeJSON(w, http.StatusCreated, m)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// MapsItemHandler dispatches every route nested under /maps/{id}/... by
// hand-parsing the path (the stdlib mux only matches the /maps/ prefix):
//
//   - /maps/{id}/version/{version}/...  (except .../bounds): serves the
//     extracted tile files for that version. The only route reachable
//     without a bearer token, when the map's anonymousAllowed is set.
//   - /maps/{id}/upload           (POST):   uploadMapVersionHandler
//   - /maps/{id}/versions         (GET):    mapVersionsHandler
//   - /maps/{id}/permissions[/{username}]:  mapPermissionsCollectionHandler /
//     mapPermissionItemHandler
//   - /maps/{id}/aliases[/{alias}]:  mapAliasesCollectionHandler /
//     mapAliasItemHandler
//   - /maps/{id}/version/{version}/bounds (GET): mapVersionBoundsHandler
//   - /maps/{id}/version/{version}/geo-objects[/{uuid}]:  geoObjectsCollectionHandler /
//     geoObjectItemHandler
//   - /maps/{id}                  (GET/PUT/DELETE): fetch/update/delete the map itself
//
// In every one of the above, {version} may be the literal keyword "current"
// (resolves to the map's MapRecord.CurrentVersion), a purely numeric real
// version identifier, or a user-defined alias created via the aliases API
// — see resolveVersionSegment for the resolution order.
//
// Every route other than the version-file route requires a bearer token.
func MapsItemHandler(st *store.Store, dataRoot string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/maps/"), "/")
		segments := strings.Split(path, "/")

		id, err := uuid.Parse(segments[0])
		if err != nil {
			http.Error(w, "invalid map id", http.StatusBadRequest)
			return
		}

		// Version file serving (but not a JSON sub-resource like .../bounds
		// or .../geo-objects[/...], see isVersionSubResourcePath) is the one
		// route that may be reached without a bearer token at all, when the
		// map itself opts in via anonymousAllowed. It's handled before the
		// blanket auth gate below so an anonymous caller can reach it; every
		// other route still requires a token.
		if isVersionFilePath(segments) {
			serveMapVersionFile(w, r, st, dataRoot, id, segments)
			return
		}

		if !requireAuthenticated(w, r) {
			return
		}

		if routeMapSubResource(w, r, st, dataRoot, id, segments) {
			return
		}

		if len(segments) != 1 {
			http.NotFound(w, r)
			return
		}

		handleMapItem(w, r, st, id)
	}
}

// isVersionFilePath reports whether segments addresses a raw extracted tile
// file under a map version, as opposed to a JSON sub-resource nested under
// one (see isVersionSubResourcePath).
func isVersionFilePath(segments []string) bool {
	return len(segments) >= 3 && segments[1] == versionPathSegment && !isVersionSubResourcePath(segments)
}

// serveMapVersionFile serves a single extracted tile file from a map
// version's directory. It's the one route reachable without a bearer token,
// when the map itself opts in via anonymousAllowed.
func serveMapVersionFile(w http.ResponseWriter, r *http.Request, st *store.Store, dataRoot string, id uuid.UUID, segments []string) {
	m, err := st.GetMap(r.Context(), id)
	if err != nil {
		writeStoreError(w, err, store.ErrMapNotFound, http.StatusNotFound, "map not found", "failed to get map")
		return
	}

	if !m.AnonymousAllowed {
		if !requireAuthenticated(w, r) || !requireMapView(w, r, st, m) {
			return
		}
	}

	version, ok := resolveVersionSegment(w, r, st, id, segments[2])
	if !ok {
		return
	}

	versionDir := tilearchive.MapVersionDir(dataRoot, id, version)
	prefix := "/maps/" + strings.Join(segments[:3], "/") + "/"
	// A map version's directory is never modified in place after upload
	// (uploadMapVersionHandler extracts into a staging dir and atomically
	// renames it into place), so its contents can be cached indefinitely by
	// clients and any CDN in front of this server.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.StripPrefix(prefix, http.FileServer(http.Dir(versionDir))).ServeHTTP(w, r)
}

// routeMapSubResource dispatches every route nested under /maps/{id}/...
// other than the bare item route and the anonymous version-file route (see
// isVersionFilePath): upload, versions, permissions[/{username}],
// aliases[/{alias}], and version/{v}/bounds|geo-objects[/{uuid}]. It returns
// true if segments matched one of these routes — the request has been fully
// handled, whether it succeeded or failed — or false if the caller should
// fall through to treating this as the bare /maps/{id} route.
func routeMapSubResource(w http.ResponseWriter, r *http.Request, st *store.Store, dataRoot string, id uuid.UUID, segments []string) bool {
	if len(segments) < 2 {
		return false
	}

	switch segments[1] {
	case "upload":
		return routeMapUpload(w, r, st, dataRoot, id, segments)
	case "versions":
		return routeMapVersions(w, r, st, id, segments)
	case "permissions":
		return routeMapPermissions(w, r, st, id, segments)
	case "aliases":
		return routeMapAliases(w, r, st, id, segments)
	case versionPathSegment:
		return routeMapVersionSubResource(w, r, st, dataRoot, id, segments)
	default:
		return false
	}
}

func routeMapUpload(w http.ResponseWriter, r *http.Request, st *store.Store, dataRoot string, id uuid.UUID, segments []string) bool {
	if len(segments) != 2 {
		return false
	}

	uploadMapVersionHandler(st, dataRoot, id)(w, r)

	return true
}

func routeMapVersions(w http.ResponseWriter, r *http.Request, st *store.Store, id uuid.UUID, segments []string) bool {
	if len(segments) != 2 {
		return false
	}

	if _, ok := getViewableMap(w, r, st, id); ok {
		mapVersionsHandler(st, id)(w, r)
	}

	return true
}

func routeMapPermissions(w http.ResponseWriter, r *http.Request, st *store.Store, id uuid.UUID, segments []string) bool {
	switch len(segments) {
	case 2:
		mapPermissionsCollectionHandler(st, id)(w, r)
	case 3:
		mapPermissionItemHandler(st, id, segments[2])(w, r)
	default:
		return false
	}

	return true
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

// routeMapVersionSubResource dispatches .../version/{v}/bounds,
// .../version/{v}/geo-objects[/{uuid}], and .../version/{v}/archive to their
// respective sub-routers below. {v} may be currentVersionKeyword or a
// user-defined alias, resolved via resolveVersionSegment.
func routeMapVersionSubResource(w http.ResponseWriter, r *http.Request, st *store.Store, dataRoot string, id uuid.UUID, segments []string) bool {
	if len(segments) < 4 {
		return false
	}

	version, ok := resolveVersionSegment(w, r, st, id, segments[2])
	if !ok {
		return true
	}

	switch segments[3] {
	case boundsPathSegment:
		return routeMapVersionBounds(w, r, st, dataRoot, id, version, segments)
	case archivePathSegment:
		return routeMapVersionArchive(w, r, st, dataRoot, id, version, segments)
	case geoObjectsPathSegment:
		return routeMapVersionGeoObjects(w, r, st, id, version, segments)
	default:
		return false
	}
}

// routeMapVersionBounds dispatches .../version/{v}/bounds.
func routeMapVersionBounds(w http.ResponseWriter, r *http.Request, st *store.Store, dataRoot string, id uuid.UUID, version string, segments []string) bool {
	if len(segments) != 4 {
		return false
	}

	if _, ok := getViewableMap(w, r, st, id); ok {
		mapVersionBoundsHandler(dataRoot, id, version)(w, r)
	}

	return true
}

// routeMapVersionArchive dispatches .../version/{v}/archive.
func routeMapVersionArchive(w http.ResponseWriter, r *http.Request, st *store.Store, dataRoot string, id uuid.UUID, version string, segments []string) bool {
	if len(segments) != 4 {
		return false
	}

	if _, ok := getViewableMap(w, r, st, id); ok {
		mapVersionArchiveHandler(dataRoot, id, version)(w, r)
	}

	return true
}

// routeMapVersionGeoObjects dispatches .../version/{v}/geo-objects[/{uuid}].
func routeMapVersionGeoObjects(w http.ResponseWriter, r *http.Request, st *store.Store, id uuid.UUID, version string, segments []string) bool {
	switch len(segments) {
	case 4:
		geoObjectsCollectionHandler(st, id, version)(w, r)

	case 5:
		geoObjID, err := uuid.Parse(segments[4])
		if err != nil {
			http.Error(w, "invalid geo object id", http.StatusBadRequest)
			return true
		}

		geoObjectItemHandler(st, id, version, geoObjID)(w, r)

	default:
		return false
	}

	return true
}

// handleMapItem serves the bare /maps/{id} route: fetch, update, or delete
// the map itself.
func handleMapItem(w http.ResponseWriter, r *http.Request, st *store.Store, id uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		m, ok := getViewableMap(w, r, st, id)
		if ok {
			writeJSON(w, http.StatusOK, m)
		}

	case http.MethodPut:
		updateMapItem(w, r, st, id)

	case http.MethodDelete:
		deleteMapItem(w, r, st, id)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
	if !decodeJSON(w, r, &req) {
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	m, err := st.UpdateMap(r.Context(), id, req.Name, req.CurrentVersion, req.VisibleToAll, req.AnonymousAllowed, usernameFromContext(r.Context()))
	if err != nil {
		writeStoreError(w, err, store.ErrMapNotFound, http.StatusNotFound, "map not found", "failed to update map")
		return
	}

	recordAudit(r, st, "update", "map", m.UUID.String(), fmt.Sprintf("name=%q visibleToAll=%v anonymousAllowed=%v", m.Name, m.VisibleToAll, m.AnonymousAllowed))

	writeJSON(w, http.StatusOK, m)
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

	recordAudit(r, st, "delete", "map", id.String(), "")

	w.WriteHeader(http.StatusNoContent)
}

// mapVersionsHandler returns the upload history for a map. If the request is
// authenticated by a scoped API key that carries a version whitelist for id,
// the result is filtered down to just those versions (see
// store.APIKeyScopedVersions) — getViewableMap has already rejected the
// request outright if id itself isn't within the key's scope.
func mapVersionsHandler(st *store.Store, id uuid.UUID) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}

		versions, err := st.ListMapVersions(r.Context(), id)
		if err != nil {
			writeStoreError(w, err, store.ErrMapNotFound, http.StatusNotFound, "map not found", "failed to list map versions")
			return
		}

		if apiKeyID, ok := apiKeyIDFromContext(r.Context()); ok {
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

		writeJSON(w, http.StatusOK, versions)
	}
}

type mapPermissionRequest struct {
	CanView             bool `json:"canView"`
	CanEdit             bool `json:"canEdit"`
	CanDelete           bool `json:"canDelete"`
	CanEditGeoObjects   bool `json:"canEditGeoObjects"`
	CanDeleteGeoObjects bool `json:"canDeleteGeoObjects"`
}

// mapPermissionsCollectionHandler lists a map's per-user permission grants.
// Managing per-map permissions is admin-only, same as the global Users API.
func mapPermissionsCollectionHandler(st *store.Store, id uuid.UUID) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r, st) {
			return
		}

		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		perms, err := st.ListMapPermissions(r.Context(), id)
		if err != nil {
			http.Error(w, "failed to list map permissions", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, perms)
	}
}

// mapPermissionItemHandler grants or revokes a single user's per-map
// permission. Managing per-map permissions is admin-only, same as the global
// Users API.
func mapPermissionItemHandler(st *store.Store, id uuid.UUID, username string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r, st) {
			return
		}

		switch r.Method {
		case http.MethodPut:
			var req mapPermissionRequest
			if !decodeJSON(w, r, &req) {
				return
			}

			p, err := st.SetMapPermission(r.Context(), id, username, req.CanView, req.CanEdit, req.CanDelete, req.CanEditGeoObjects, req.CanDeleteGeoObjects, usernameFromContext(r.Context()))
			if err != nil {
				writeStoreError(w, err, store.ErrMapPermissionInvalid, http.StatusBadRequest, "map or username does not exist", "failed to set map permission")
				return
			}

			recordAudit(r, st, "grant", "map_permission", id.String()+":"+username, fmt.Sprintf("view=%v edit=%v delete=%v editGeo=%v deleteGeo=%v", req.CanView, req.CanEdit, req.CanDelete, req.CanEditGeoObjects, req.CanDeleteGeoObjects))

			writeJSON(w, http.StatusOK, p)

		case http.MethodDelete:
			if err := st.DeleteMapPermission(r.Context(), id, username); err != nil {
				http.Error(w, "failed to delete map permission", http.StatusInternalServerError)
				return
			}

			recordAudit(r, st, "revoke", "map_permission", id.String()+":"+username, "")

			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}
