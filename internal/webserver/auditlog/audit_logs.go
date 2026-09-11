// Package auditlog records best-effort audit log entries for mutating requests.
package auditlog

import (
	"log"
	"net/http"

	"nilswitt.dev/tileserve-go/internal/auth"
	"nilswitt.dev/tileserve-go/internal/httputil"
	"nilswitt.dev/tileserve-go/internal/store"
)

// RecordAudit best-effort records one audit log entry, attributed to the
// acting user of r, describing a mutating action that has just succeeded.
// Persisting the entry is secondary to the request it's describing: a
// failure here is logged (mirroring package sync's LogStore precedent, where
// observability is additive, not load-bearing) rather than turned into an
// error response for an action that has already taken effect.
func RecordAudit(r *http.Request, st *store.Store, action, entityType, entityID, detail string) {
	actor := auth.UsernameFromContext(r.Context())

	if err := st.RecordAuditLog(r.Context(), actor, action, entityType, entityID, detail); err != nil {
		// actor/entityID may carry attacker-influenced content (a username,
		// or a composite id built from one); %q (rather than %s) escapes any
		// embedded newline so it can't forge what looks like a second,
		// unrelated log line.
		log.Printf("record audit log (actor=%q action=%s entityType=%s entityId=%q): %v", actor, action, entityType, entityID, err)
	}
}

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

		httputil.WriteJSON(w, http.StatusOK, entries)
	}
}

// auditLogFilterFromQuery builds a store.AuditLogFilter from r's query
// parameters, writing a 400 response and returning ok=false if a since/until
// timestamp isn't valid RFC 3339, or limit/offset isn't a valid non-negative
// integer.
func auditLogFilterFromQuery(w http.ResponseWriter, r *http.Request) (filter store.AuditLogFilter, ok bool) {
	since, ok := httputil.QueryTimeParam(w, r, "since")
	if !ok {
		return store.AuditLogFilter{}, false
	}

	until, ok := httputil.QueryTimeParam(w, r, "until")
	if !ok {
		return store.AuditLogFilter{}, false
	}

	limit, ok := httputil.QueryIntParam(w, r, "limit")
	if !ok {
		return store.AuditLogFilter{}, false
	}

	offset, ok := httputil.QueryIntParam(w, r, "offset")
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
