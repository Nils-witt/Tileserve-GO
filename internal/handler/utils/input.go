package utils

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"nilswitt.dev/tileserve-go/internal/store"
)

type contextKey string

const (
	usernameContextKey contextKey = "username"
	apiKeyIDContextKey contextKey = "apiKeyID"
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

// RequireAuthenticated rejects a request with no authenticated user with a
// 401. It returns true when the caller may continue.
func RequireAuthenticated(w http.ResponseWriter, r *http.Request) bool {
	if UsernameFromContext(r.Context()) == "" {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return false
	}

	return true
}

// GetPermissionsOrFail looks up the authenticated caller's permissions,
// writing a 500 response and returning ok=false on failure.
func GetPermissionsOrFail(w http.ResponseWriter, r *http.Request, st *store.Store) (perms store.Permissions, ok bool) {
	perms, err := st.GetPermissions(r.Context(), UsernameFromContext(r.Context()))
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

// UsernameFromContext returns the username stored in ctx by
// ContextWithUsername, or "" if none is present.
func UsernameFromContext(ctx context.Context) string {
	username, _ := ctx.Value(usernameContextKey).(string)
	return username
}

// APIKeyIDFromContext returns the API key ID stored in ctx by
// ContextWithAPIKeyID.
func APIKeyIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(apiKeyIDContextKey).(uuid.UUID)
	return id, ok
}

// ContextWithUsername returns a copy of ctx carrying username, retrievable
// via UsernameFromContext.
func ContextWithUsername(ctx context.Context, username string) context.Context {
	return context.WithValue(ctx, usernameContextKey, username)
}

// ContextWithAPIKeyID returns a copy of ctx carrying id, retrievable via
// APIKeyIDFromContext.
func ContextWithAPIKeyID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, apiKeyIDContextKey, id)
}

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
