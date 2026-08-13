package handler

import (
	"net/http"

	"github.com/google/uuid"

	"nilswitt.dev/tileserve-go/internal/store"
)

type apiKeyRequest struct {
	Name string `json:"name"`
}

// apiKeyCreatedResponse embeds a created key's record plus its plaintext
// value, which is returned exactly once (at creation) and never again — the
// same principle as never re-exposing a password hash or refresh token.
type apiKeyCreatedResponse struct {
	store.APIKeyRecord
	Key string `json:"key"`
}

// routeUserAPIKeys dispatches /users/{username}/api-keys[/{id}], mirroring
// routeMapAliases's shape. The caller (UserItemHandler) has already verified
// the request is from an admin.
func routeUserAPIKeys(w http.ResponseWriter, r *http.Request, st *store.Store, username string, segments []string) {
	switch len(segments) {
	case 2:
		apiKeysCollectionHandler(st, username)(w, r)
	case 3:
		apiKeyItemHandler(st, username, segments[2])(w, r)
	default:
		http.NotFound(w, r)
	}
}

// apiKeysCollectionHandler lists (GET) or creates (POST) API keys for
// username. Admin-only, same as the rest of the /users API.
func apiKeysCollectionHandler(st *store.Store, username string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			keys, err := st.ListAPIKeys(r.Context(), username)
			if err != nil {
				http.Error(w, "failed to list api keys", http.StatusInternalServerError)
				return
			}

			writeJSON(w, http.StatusOK, keys)

		case http.MethodPost:
			var req apiKeyRequest
			if !decodeJSON(w, r, &req) {
				return
			}

			plainKey, rec, err := st.CreateAPIKey(r.Context(), username, req.Name, usernameFromContext(r.Context()))
			if err != nil {
				writeStoreError(w, err, store.ErrUserNotFound, http.StatusNotFound, "user not found", "failed to create api key")
				return
			}

			writeJSON(w, http.StatusCreated, apiKeyCreatedResponse{APIKeyRecord: rec, Key: plainKey})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// apiKeyItemHandler revokes (DELETE) a single API key belonging to username.
func apiKeyItemHandler(st *store.Store, username, idStr string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodDelete) {
			return
		}

		id, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "invalid api key id", http.StatusBadRequest)
			return
		}

		if err := st.RevokeAPIKey(r.Context(), username, id); err != nil {
			writeStoreError(w, err, store.ErrAPIKeyNotFound, http.StatusNotFound, "api key not found", "failed to revoke api key")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
