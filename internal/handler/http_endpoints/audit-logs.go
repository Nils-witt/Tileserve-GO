// Package http_endpoints wires HTTP handlers for API resources to the store.
package http_endpoints

import (
	"net/http"

	"nilswitt.dev/tileserve-go/internal/handler/utils"
	"nilswitt.dev/tileserve-go/internal/store"
)

// AuditLogsCollectionHandler serves GET /audit-logs: an admin-only listing
// of audit log entries, optionally filtered via auditLogFilterFromQuery.
func AuditLogsCollectionHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter, ok := auditLogFilterFromQuery(w, r)
		if !ok {
			return
		}

		entries, err := st.ListAuditLogs(r.Context(), filter)
		if err != nil {
			http.Error(w, "failed to list audit logs", http.StatusInternalServerError)
			return
		}

		utils.WriteJSON(w, http.StatusOK, entries)
	}
}

// auditLogFilterFromQuery builds a store.AuditLogFilter from r's query
// parameters, writing a 400 response and returning ok=false if a since/until
// timestamp isn't valid RFC 3339, or limit/offset isn't a valid non-negative
// integer.
func auditLogFilterFromQuery(w http.ResponseWriter, r *http.Request) (filter store.AuditLogFilter, ok bool) {
	since, ok := utils.QueryTimeParam(w, r, "since")
	if !ok {
		return store.AuditLogFilter{}, false
	}

	until, ok := utils.QueryTimeParam(w, r, "until")
	if !ok {
		return store.AuditLogFilter{}, false
	}

	limit, ok := utils.QueryIntParam(w, r, "limit")
	if !ok {
		return store.AuditLogFilter{}, false
	}

	offset, ok := utils.QueryIntParam(w, r, "offset")
	if !ok {
		return store.AuditLogFilter{}, false
	}

	return store.AuditLogFilter{
		Actor:      r.URL.Query().Get("actor"),
		Action:     r.URL.Query().Get("action"),
		EntityType: r.URL.Query().Get("entityType"),
		EntityID:   r.URL.Query().Get("entityId"),
		Since:      since,
		Until:      until,
		Limit:      limit,
		Offset:     offset,
	}, true
}
