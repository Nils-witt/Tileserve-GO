// Package utils holds small HTTP request/response helpers shared across
// tileserve-go's handlers.
package utils

import "net/http"

// RequireMethod rejects a request whose method isn't method with a 405. It
// returns true when the caller may continue.
func RequireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}

	return true
}
