// Package maps implements map listing and filtering shared by the maps API.
package maps

import (
	"context"
	"net/http"

	"nilswitt.dev/tileserve-go/internal/handler/utils"
	"nilswitt.dev/tileserve-go/internal/store"
)

func filterMapsByAPIKeyScope(ctx context.Context, st *store.Store, maps []store.MapRecord) ([]store.MapRecord, error) {
	apiKeyID, ok := utils.APIKeyIDFromContext(ctx)
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

// ListMaps writes the maps visible to the caller, honoring both stored
// visibility rules and any API-key scope restriction, as JSON.
func ListMaps(w http.ResponseWriter, r *http.Request, st *store.Store) {
	username := utils.UsernameFromContext(r.Context())

	perms, ok := utils.GetPermissionsOrFail(w, r, st)
	if !ok {
		return
	}

	bypassVisibility := perms.GrantsMapVisibility()

	visibleToAll, ok := utils.QueryBoolParam(w, r, "visibleToAll")
	if !ok {
		return
	}

	anonymousAllowed, ok := utils.QueryBoolParam(w, r, "anonymousAllowed")
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

	utils.WriteJSON(w, http.StatusOK, maps)
}
