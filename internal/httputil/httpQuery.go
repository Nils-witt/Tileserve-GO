// Package httputil provides shared, dependency-free HTTP request-handling helpers.
package httputil

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// RequireMethod rejects a request whose method isn't method with a 405. It
// returns true when the caller may continue.
func RequireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}

	return true
}

// QueryBoolParam parses the optional query parameter name as a bool. A
// missing parameter returns (nil, true).
func QueryBoolParam(w http.ResponseWriter, r *http.Request, name string) (value *bool, ok bool) {
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

// QueryFloatParam parses the optional query parameter name as a float64. A
// missing parameter returns (nil, true).
func QueryFloatParam(w http.ResponseWriter, r *http.Request, name string) (value *float64, ok bool) {
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

// QueryTimeParam parses the optional query parameter name as an RFC 3339
// timestamp. A missing parameter returns (nil, true).
func QueryTimeParam(w http.ResponseWriter, r *http.Request, name string) (value *time.Time, ok bool) {
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

// QueryIntParam parses the optional query parameter name as a non-negative
// int. A missing parameter returns (0, true).
func QueryIntParam(w http.ResponseWriter, r *http.Request, name string) (value int, ok bool) {
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
