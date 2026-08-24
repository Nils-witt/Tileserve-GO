package handler

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"nilswitt.dev/tileserve-go/internal/store"
)

// recordAudit best-effort records one audit log entry, attributed to the
// acting user of r, describing a mutating action that has just succeeded.
// Persisting the entry is secondary to the request it's describing: a
// failure here is logged (mirroring package sync's LogStore precedent, where
// observability is additive, not load-bearing) rather than turned into an
// error response for an action that has already taken effect.
func recordAudit(r *http.Request, st *store.Store, action, entityType, entityID, detail string) {
	actor := usernameFromContext(r.Context())

	if err := st.RecordAuditLog(r.Context(), actor, action, entityType, entityID, detail); err != nil {
		// actor/entityID may carry attacker-influenced content (a username,
		// or a composite id built from one); %q (rather than %s) escapes any
		// embedded newline so it can't forge what looks like a second,
		// unrelated log line.
		//nolint:gosec // G706: actor/entityID are quoted via %q precisely to neutralize this
		log.Printf("record audit log (actor=%q action=%s entityType=%s entityId=%q): %v", actor, action, entityType, entityID, err)
	}
}

// auditLogFilterFromQuery builds a store.AuditLogFilter from r's query
// parameters, writing a 400 response and returning ok=false if a since/until
// timestamp isn't valid RFC 3339, or limit/offset isn't a valid non-negative
// integer.
func auditLogFilterFromQuery(w http.ResponseWriter, r *http.Request) (filter store.AuditLogFilter, ok bool) {
	since, ok := queryTimeParam(w, r, "since")
	if !ok {
		return store.AuditLogFilter{}, false
	}

	until, ok := queryTimeParam(w, r, "until")
	if !ok {
		return store.AuditLogFilter{}, false
	}

	limit, ok := queryIntParam(w, r, "limit")
	if !ok {
		return store.AuditLogFilter{}, false
	}

	offset, ok := queryIntParam(w, r, "offset")
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

// queryTimeParam parses the optional query parameter name as an RFC 3339
// timestamp. A missing parameter returns (nil, true).
func queryTimeParam(w http.ResponseWriter, r *http.Request, name string) (value *time.Time, ok bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil, true
	}

	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		http.Error(w, "invalid "+name+": must be an RFC 3339 timestamp", http.StatusBadRequest)
		return nil, false
	}

	return &t, true
}

// queryIntParam parses the optional query parameter name as a non-negative
// int. A missing parameter returns (0, true).
func queryIntParam(w http.ResponseWriter, r *http.Request, name string) (value int, ok bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, true
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		http.Error(w, "invalid "+name+": must be a non-negative integer", http.StatusBadRequest)
		return 0, false
	}

	return n, true
}

// AuditLogsCollectionHandler serves the /audit-logs collection route
// (admin-only): GET lists recorded audit entries, most recent first,
// optionally filtered by actor/action/entityType/entityId/since/until and
// paginated via limit/offset.
func AuditLogsCollectionHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r, st) {
			return
		}

		if !requireMethod(w, r, http.MethodGet) {
			return
		}

		filter, ok := auditLogFilterFromQuery(w, r)
		if !ok {
			return
		}

		entries, err := st.ListAuditLogs(r.Context(), filter)
		if err != nil {
			http.Error(w, "failed to list audit logs", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, entries)
	}
}
