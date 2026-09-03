// Package auditlog records best-effort audit log entries for mutating requests.
package auditlog

import (
	"log"
	"net/http"

	"nilswitt.dev/tileserve-go/internal/handler/utils"
	"nilswitt.dev/tileserve-go/internal/store"
)

// RecordAudit best-effort records one audit log entry, attributed to the
// acting user of r, describing a mutating action that has just succeeded.
// Persisting the entry is secondary to the request it's describing: a
// failure here is logged (mirroring package sync's LogStore precedent, where
// observability is additive, not load-bearing) rather than turned into an
// error response for an action that has already taken effect.
func RecordAudit(r *http.Request, st *store.Store, action, entityType, entityID, detail string) {
	actor := utils.UsernameFromContext(r.Context())

	if err := st.RecordAuditLog(r.Context(), actor, action, entityType, entityID, detail); err != nil {
		// actor/entityID may carry attacker-influenced content (a username,
		// or a composite id built from one); %q (rather than %s) escapes any
		// embedded newline so it can't forge what looks like a second,
		// unrelated log line.
		log.Printf("record audit log (actor=%q action=%s entityType=%s entityId=%q): %v", actor, action, entityType, entityID, err)
	}
}
