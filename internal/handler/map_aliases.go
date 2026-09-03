package handler

import (
	"net/http"

	"nilswitt.dev/tileserve-go/internal/handler/auditlog"
	"nilswitt.dev/tileserve-go/internal/handler/utils"

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

// MapAliasesListHandler serves GET /maps/{id}/aliases: a map's version
// aliases. Unlike permissions, alias management is NOT admin-only: viewing
// follows the same rule as other map-scoped reads (getViewableMap), and
// creating/updating/deleting an alias follows the same
// requireMapPermission(CanEdit) rule as updateMapItem, since editing
// currentVersion itself only requires can_edit.
func MapAliasesListHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := utils.PathUUID(w, r, "id", "map id")
		if !ok {
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

		utils.WriteJSON(w, http.StatusOK, aliases)
	}
}

// MapAliasGetHandler serves GET /maps/{id}/aliases/{alias}: fetches a single
// named alias (requires view access).
func MapAliasGetHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := utils.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		if _, ok := getViewableMap(w, r, st, id); !ok {
			return
		}

		alias := r.PathValue("alias")

		version, err := st.GetMapVersionAlias(r.Context(), id, alias)
		if err != nil {
			writeStoreError(w, err, store.ErrMapVersionAliasNotFound, http.StatusNotFound, "alias not found", "failed to get alias")
			return
		}

		utils.WriteJSON(w, http.StatusOK, store.MapVersionAlias{Alias: alias, Version: version})
	}
}

// MapAliasSetHandler serves PUT /maps/{id}/aliases/{alias}: creates or
// replaces a single named alias (requires can_edit).
func MapAliasSetHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := utils.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		if !requireMapPermission(w, r, st, id,
			func(p store.Permissions) bool { return p.CanEdit },
			func(mp store.MapPermission) bool { return mp.CanEdit },
		) {
			return
		}

		alias := r.PathValue("alias")
		if !validateAliasName(w, alias) {
			return
		}

		var req mapAliasRequest
		if !utils.DecodeJSON(w, r, &req) {
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

		auditlog.RecordAudit(r, st, "update", "map_alias", id.String()+":"+alias, "version="+req.Version)

		utils.WriteJSON(w, http.StatusOK, a)
	}
}

// MapAliasDeleteHandler serves DELETE /maps/{id}/aliases/{alias}: deletes a
// single named alias (requires can_edit).
func MapAliasDeleteHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := utils.PathUUID(w, r, "id", "map id")
		if !ok {
			return
		}

		if !requireMapPermission(w, r, st, id,
			func(p store.Permissions) bool { return p.CanEdit },
			func(mp store.MapPermission) bool { return mp.CanEdit },
		) {
			return
		}

		alias := r.PathValue("alias")

		if err := st.DeleteMapVersionAlias(r.Context(), id, alias); err != nil {
			http.Error(w, "failed to delete alias", http.StatusInternalServerError)
			return
		}

		auditlog.RecordAudit(r, st, "delete", "map_alias", id.String()+":"+alias, "")

		w.WriteHeader(http.StatusNoContent)
	}
}
