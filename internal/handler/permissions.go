package handler

import (
	"net/http"

	"nilswitt.dev/tileserve-go/internal/store"
)

// PermissionsCollectionHandler serves the /permissions route (admin-only):
// GET lists every per-map permission grant across every map, each paired
// with the map it applies to. It's the global counterpart to
// GET /maps/{id}/permissions, letting an admin audit all per-map grants
// without listing them one map at a time.
func PermissionsCollectionHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r, st) {
			return
		}

		if !requireMethod(w, r, http.MethodGet) {
			return
		}

		perms, err := st.ListAllMapPermissions(r.Context())
		if err != nil {
			http.Error(w, "failed to list permissions", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, perms)
	}
}
