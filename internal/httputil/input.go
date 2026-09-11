package httputil

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
)

// DecodeJSON decodes r's JSON body into v, writing a 400 response and
// returning false on failure.
func DecodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}

	return true
}

// PathUUID parses r's {name} mux wildcard as a uuid.UUID, writing a 400
// response (using label as the human-readable subject, e.g. "map id") and
// returning ok=false if it isn't one.
func PathUUID(w http.ResponseWriter, r *http.Request, name, label string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		http.Error(w, "invalid "+label, http.StatusBadRequest)
		return uuid.UUID{}, false
	}

	return id, true
}
