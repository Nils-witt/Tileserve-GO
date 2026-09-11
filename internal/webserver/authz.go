package webserver

import (
	"net/http"

	"nilswitt.dev/tileserve-go/internal/auth"
	"nilswitt.dev/tileserve-go/internal/store"
)

// RequireAuthenticated rejects a request with no authenticated user with a
// 401. It returns true when the caller may continue.
func RequireAuthenticated(w http.ResponseWriter, r *http.Request) bool {
	if auth.UsernameFromContext(r.Context()) == "" {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return false
	}

	return true
}

// GetPermissionsOrFail looks up the authenticated caller's permissions,
// writing a 500 response and returning ok=false on failure.
func GetPermissionsOrFail(w http.ResponseWriter, r *http.Request, st *store.Store) (perms store.Permissions, ok bool) {
	perms, err := st.GetPermissions(r.Context(), auth.UsernameFromContext(r.Context()))
	if err != nil {
		http.Error(w, "failed to check permissions", http.StatusInternalServerError)
		return store.Permissions{}, false
	}

	return perms, true
}

// RequirePermission rejects a request whose caller's permissions don't
// satisfy allowed with a 403 (or a 500 if permissions can't be checked). It
// returns true when the caller may continue.
func RequirePermission(w http.ResponseWriter, r *http.Request, st *store.Store, allowed func(store.Permissions) bool) bool {
	perms, ok := GetPermissionsOrFail(w, r, st)
	if !ok {
		return false
	}

	if !allowed(perms) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}

	return true
}

// RequireAdmin rejects a request whose caller isn't an admin with a 403. It
// returns true when the caller may continue.
func RequireAdmin(w http.ResponseWriter, r *http.Request, st *store.Store) bool {
	return RequirePermission(w, r, st, func(p store.Permissions) bool { return p.IsAdmin })
}
