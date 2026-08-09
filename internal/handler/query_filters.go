package handler

import (
	"fmt"
	"net/http"
	"strconv"
)

// queryBoolParam parses the optional query parameter name as a bool. A
// missing parameter returns (nil, true). If present but not a valid bool it
// writes a 400 response and returns ok=false.
func queryBoolParam(w http.ResponseWriter, r *http.Request, name string) (value *bool, ok bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil, true
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid %s: must be true or false", name), http.StatusBadRequest)
		return nil, false
	}
	return &b, true
}

// queryFloatParam is the float64 equivalent of queryBoolParam.
func queryFloatParam(w http.ResponseWriter, r *http.Request, name string) (value *float64, ok bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil, true
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid %s: must be a number", name), http.StatusBadRequest)
		return nil, false
	}
	return &f, true
}
