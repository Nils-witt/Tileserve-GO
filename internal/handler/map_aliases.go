package handler

import (
	"net/http"

	"github.com/google/uuid"

	"nilswitt.dev/tileserve-go/internal/store"
	"nilswitt.dev/tileserve-go/internal/tilearchive"
)

// mapAliasRequest is the PUT body for creating/updating an alias.
type mapAliasRequest struct {
	Version string `json:"version"`
}

// validateAliasName rejects an alias name that would collide with the
// reserved "current" keyword or be ambiguous with a real (always-numeric)
// version identifier. It writes a 400 and returns false if invalid.
func validateAliasName(w http.ResponseWriter, alias string) bool {
	if alias == "" {
		http.Error(w, "alias is required", http.StatusBadRequest)
		return false
	}

	if alias == currentVersionKeyword {
		http.Error(w, `alias may not be "current" (reserved keyword)`, http.StatusBadRequest)
		return false
	}

	if tilearchive.NumericSegmentRE.MatchString(alias) {
		http.Error(w, "alias may not be purely numeric (would be ambiguous with a real version)", http.StatusBadRequest)
		return false
	}

	return true
}

// routeMapAliases dispatches /maps/{id}/aliases[/{alias}], mirroring
// routeMapPermissions's shape.
func routeMapAliases(w http.ResponseWriter, r *http.Request, st *store.Store, id uuid.UUID, segments []string) bool {
	switch len(segments) {
	case 2:
		mapAliasesCollectionHandler(st, id)(w, r)
	case 3:
		mapAliasItemHandler(st, id, segments[2])(w, r)
	default:
		return false
	}

	return true
}

// mapAliasesCollectionHandler lists a map's version aliases. Unlike
// permissions, alias management is NOT admin-only: viewing follows the same
// rule as other map-scoped reads (getViewableMap), and creating/updating/
// deleting an alias follows the same requireMapPermission(CanEdit) rule as
// updateMapItem, since editing currentVersion itself only requires can_edit.
func mapAliasesCollectionHandler(st *store.Store, id uuid.UUID) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}

		if _, ok := getViewableMap(w, r, st, id); !ok {
			return
		}

		aliases, err := st.ListMapVersionAliases(r.Context(), id)
		if err != nil {
			http.Error(w, "failed to list aliases", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, aliases)
	}
}

// mapAliasItemHandler fetches (GET, requires view access), creates/replaces
// (PUT, requires can_edit), or deletes (DELETE, requires can_edit) a single
// named alias.
func mapAliasItemHandler(st *store.Store, id uuid.UUID, alias string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if _, ok := getViewableMap(w, r, st, id); !ok {
				return
			}

			version, err := st.GetMapVersionAlias(r.Context(), id, alias)
			if err != nil {
				writeStoreError(w, err, store.ErrMapVersionAliasNotFound, http.StatusNotFound, "alias not found", "failed to get alias")
				return
			}

			writeJSON(w, http.StatusOK, store.MapVersionAlias{Alias: alias, Version: version})

		case http.MethodPut:
			if !requireMapPermission(w, r, st, id,
				func(p store.Permissions) bool { return p.CanEdit },
				func(mp store.MapPermission) bool { return mp.CanEdit },
			) {
				return
			}

			if !validateAliasName(w, alias) {
				return
			}

			var req mapAliasRequest
			if !decodeJSON(w, r, &req) {
				return
			}

			if req.Version == "" {
				http.Error(w, "version is required", http.StatusBadRequest)
				return
			}

			a, err := st.SetMapVersionAlias(r.Context(), id, alias, req.Version, usernameFromContext(r.Context()))
			if err != nil {
				writeStoreError(w, err, store.ErrMapVersionAliasInvalid, http.StatusBadRequest, "map or version does not exist", "failed to set alias")
				return
			}

			recordAudit(r, st, "update", "map_alias", id.String()+":"+alias, "version="+req.Version)

			writeJSON(w, http.StatusOK, a)

		case http.MethodDelete:
			if !requireMapPermission(w, r, st, id,
				func(p store.Permissions) bool { return p.CanEdit },
				func(mp store.MapPermission) bool { return mp.CanEdit },
			) {
				return
			}

			if err := st.DeleteMapVersionAlias(r.Context(), id, alias); err != nil {
				http.Error(w, "failed to delete alias", http.StatusInternalServerError)
				return
			}

			recordAudit(r, st, "delete", "map_alias", id.String()+":"+alias, "")

			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}
