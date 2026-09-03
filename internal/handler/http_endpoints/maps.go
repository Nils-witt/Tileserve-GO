package http_endpoints

import (
	"fmt"
	"net/http"

	"nilswitt.dev/tileserve-go/internal/handler/auditlog"
	"nilswitt.dev/tileserve-go/internal/handler/utils"
	"nilswitt.dev/tileserve-go/internal/maps"
	"nilswitt.dev/tileserve-go/internal/store"
)

// MapsList returns a handler that lists maps visible to the caller.
func MapsList(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		maps.ListMaps(w, r, st)
	}
}

// MapCreate returns a handler that creates a new map.
func MapCreate(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !utils.RequirePermission(w, r, st, func(p store.Permissions) bool { return p.CanCreate }) {
			return
		}

		var req maps.MapRequest
		if !utils.DecodeJSON(w, r, &req) {
			return
		}

		if req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}

		m, err := st.CreateMap(r.Context(), req.Name, req.CurrentVersion, req.VisibleToAll, req.AnonymousAllowed, utils.UsernameFromContext(r.Context()))
		if err != nil {
			http.Error(w, "failed to create map", http.StatusInternalServerError)
			return
		}

		auditlog.RecordAudit(r, st, "create", "map", m.UUID.String(), fmt.Sprintf("name=%q visibleToAll=%v anonymousAllowed=%v", m.Name, m.VisibleToAll, m.AnonymousAllowed))

		utils.WriteJSON(w, http.StatusCreated, m)
	}
}
