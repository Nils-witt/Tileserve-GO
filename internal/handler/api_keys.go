package handler

import (
	"errors"
	"fmt"
	"net/http"

	"nilswitt.dev/tileserve-go/internal/handler/auditlog"
	"nilswitt.dev/tileserve-go/internal/handler/utils"

	"nilswitt.dev/tileserve-go/internal/store"
)

// apiKeyRequest carries a caller-generated public key to register — the
// server never sees or stores a private key (see store.CreateAPIKey).
type apiKeyRequest struct {
	Name         string `json:"name"`
	PublicKeyPEM string `json:"publicKeyPem"`
}

// APIKeysListHandler serves GET /users/{username}/api-keys (admin-only):
// lists API keys for username.
func APIKeysListHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := r.PathValue("username")

		keys, err := st.ListAPIKeys(r.Context(), username)
		if err != nil {
			http.Error(w, "failed to list api keys", http.StatusInternalServerError)
			return
		}

		utils.WriteJSON(w, http.StatusOK, keys)
	}
}

// APIKeyCreateHandler serves POST /users/{username}/api-keys (admin-only):
// registers a new API key for username.
func APIKeyCreateHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := r.PathValue("username")

		var req apiKeyRequest
		if !utils.DecodeJSON(w, r, &req) {
			return
		}

		if req.PublicKeyPEM == "" {
			http.Error(w, "publicKeyPem is required", http.StatusBadRequest)
			return
		}

		rec, err := st.CreateAPIKey(r.Context(), username, req.Name, usernameFromContext(r.Context()), req.PublicKeyPEM)
		if err != nil {
			if errors.Is(err, store.ErrInvalidPublicKeyPEM) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			writeStoreError(w, err, store.ErrUserNotFound, http.StatusNotFound, "user not found", "failed to create api key")

			return
		}

		auditlog.RecordAudit(r, st, "create", "api_key", rec.ID.String(), fmt.Sprintf("owner=%s name=%q", username, req.Name))

		utils.WriteJSON(w, http.StatusCreated, rec)
	}
}

// APIKeyDeleteHandler serves DELETE /users/{username}/api-keys/{id}
// (admin-only): revokes a single API key belonging to username.
func APIKeyDeleteHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := r.PathValue("username")

		id, ok := utils.PathUUID(w, r, "id", "api key id")
		if !ok {
			return
		}

		if err := st.RevokeAPIKey(r.Context(), username, id); err != nil {
			writeStoreError(w, err, store.ErrAPIKeyNotFound, http.StatusNotFound, "api key not found", "failed to revoke api key")
			return
		}

		auditlog.RecordAudit(r, st, "revoke", "api_key", id.String(), "owner="+username)

		w.WriteHeader(http.StatusNoContent)
	}
}

type apiKeyScopeRequest struct {
	Versions []string `json:"versions"`
}

// APIKeyScopesListHandler serves GET
// /users/{username}/api-keys/{id}/scopes (admin-only).
func APIKeyScopesListHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := r.PathValue("username")

		id, ok := utils.PathUUID(w, r, "id", "api key id")
		if !ok {
			return
		}

		scopes, err := st.ListAPIKeyScopes(r.Context(), username, id)
		if err != nil {
			utils.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list api key scopes"})
			return
		}

		utils.WriteJSON(w, http.StatusOK, scopes)
	}
}

// APIKeyScopesClearHandler serves DELETE
// /users/{username}/api-keys/{id}/scopes (admin-only): clears every scope
// grant for the key, reverting it to unrestricted (all-maps) access.
func APIKeyScopesClearHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := r.PathValue("username")

		id, ok := utils.PathUUID(w, r, "id", "api key id")
		if !ok {
			return
		}

		if err := st.ClearAPIKeyScope(r.Context(), username, id); err != nil {
			writeStoreError(w, err, store.ErrAPIKeyNotFound, http.StatusNotFound, "api key not found", "failed to clear api key scope")
			return
		}

		auditlog.RecordAudit(r, st, "revoke", "api_key_scope", id.String(), "owner="+username+" cleared all scopes")

		w.WriteHeader(http.StatusNoContent)
	}
}

// APIKeyScopeSetHandler serves PUT
// /users/{username}/api-keys/{id}/scopes/{mapId} (admin-only): grants (or
// replaces) a scope restricting the key to specific versions of mapId.
func APIKeyScopeSetHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := r.PathValue("username")

		id, ok := utils.PathUUID(w, r, "id", "api key id")
		if !ok {
			return
		}

		mapID, ok := utils.PathUUID(w, r, "mapId", "map id")
		if !ok {
			return
		}

		var req apiKeyScopeRequest
		if !utils.DecodeJSON(w, r, &req) {
			return
		}

		scope, err := st.SetAPIKeyScope(r.Context(), username, id, mapID, req.Versions)
		if err != nil {
			if errors.Is(err, store.ErrAPIKeyScopeInvalid) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			writeStoreError(w, err, store.ErrAPIKeyNotFound, http.StatusNotFound, "api key not found", "failed to set api key scope")

			return
		}

		auditlog.RecordAudit(r, st, "grant", "api_key_scope", id.String()+":"+mapID.String(), fmt.Sprintf("owner=%s versions=%v", username, req.Versions))

		utils.WriteJSON(w, http.StatusOK, scope)
	}
}

// APIKeyScopeDeleteHandler serves DELETE
// /users/{username}/api-keys/{id}/scopes/{mapId} (admin-only): revokes the
// key's scope grant for a single map.
func APIKeyScopeDeleteHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := r.PathValue("username")

		id, ok := utils.PathUUID(w, r, "id", "api key id")
		if !ok {
			return
		}

		mapID, ok := utils.PathUUID(w, r, "mapId", "map id")
		if !ok {
			return
		}

		if err := st.DeleteAPIKeyScope(r.Context(), username, id, mapID); err != nil {
			writeStoreError(w, err, store.ErrAPIKeyNotFound, http.StatusNotFound, "api key not found", "failed to delete api key scope")
			return
		}

		auditlog.RecordAudit(r, st, "revoke", "api_key_scope", id.String()+":"+mapID.String(), "owner="+username)

		w.WriteHeader(http.StatusNoContent)
	}
}
