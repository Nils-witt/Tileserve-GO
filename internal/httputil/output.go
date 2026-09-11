package httputil

import (
	"encoding/json"
	"net/http"
)

// WriteJSON writes v as a JSON response body with the given HTTP status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
