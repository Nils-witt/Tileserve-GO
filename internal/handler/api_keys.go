package handler

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"nilswitt.dev/tileserve-go/internal/store"
)

// apiKeyRequest carries a caller-generated public key to register — the
// server never sees or stores a private key (see store.CreateAPIKey).
type apiKeyRequest struct {
	Name         string `json:"name"`
	PublicKeyPEM string `json:"publicKeyPem"`
}

// routeUserAPIKeys dispatches /users/{username}/api-keys[/{id}[/scopes[/{mapId}]]],
// mirroring routeMapAliases's shape. The caller (UserItemHandler) has already
// verified the request is from an admin.
func routeUserAPIKeys(w http.ResponseWriter, r *http.Request, st *store.Store, username string, segments []string) {
	switch len(segments) {
	case 2:
		apiKeysCollectionHandler(st, username)(w, r)
	case 3:
		apiKeyItemHandler(st, username, segments[2])(w, r)
	case 4:
		if segments[3] != "scopes" {
			http.NotFound(w, r)
			return
		}

		apiKeyScopesCollectionHandler(st, username, segments[2])(w, r)
	case 5:
		if segments[3] != "scopes" {
			http.NotFound(w, r)
			return
		}

		apiKeyScopeItemHandler(st, username, segments[2], segments[4])(w, r)
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

			writeJSON(w, http.StatusCreated, rec)

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

// apiKeyScopeRequest carries a scope grant's version whitelist: omitted or
// null means every version of the map is in scope.
type apiKeyScopeRequest struct {
	Versions []string `json:"versions"`
}

// apiKeyScopesCollectionHandler lists (GET) a key's scope entries, or clears
// all of them (DELETE), restoring the key to unrestricted access (see
// store.ClearAPIKeyScope). Admin-only, same as the rest of the /users API.
func apiKeyScopesCollectionHandler(st *store.Store, username, idStr string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "invalid api key id", http.StatusBadRequest)
			return
		}

		switch r.Method {
		case http.MethodGet:
			scopes, err := st.ListAPIKeyScopes(r.Context(), username, id)
			if err != nil {
				http.Error(w, "failed to list api key scopes", http.StatusInternalServerError)
				return
			}

			writeJSON(w, http.StatusOK, scopes)

		case http.MethodDelete:
			if err := st.ClearAPIKeyScope(r.Context(), username, id); err != nil {
				writeStoreError(w, err, store.ErrAPIKeyNotFound, http.StatusNotFound, "api key not found", "failed to clear api key scope")
				return
			}

			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// apiKeyScopeItemHandler grants (PUT) or revokes (DELETE) a key's access to
// a single map. Admin-only, same as the rest of the /users API.
func apiKeyScopeItemHandler(st *store.Store, username, idStr, mapIDStr string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "invalid api key id", http.StatusBadRequest)
			return
		}

		mapID, err := uuid.Parse(mapIDStr)
		if err != nil {
			http.Error(w, "invalid map id", http.StatusBadRequest)
			return
		}

		switch r.Method {
		case http.MethodPut:
			var req apiKeyScopeRequest
			if !decodeJSON(w, r, &req) {
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

			writeJSON(w, http.StatusOK, scope)

		case http.MethodDelete:
			if err := st.DeleteAPIKeyScope(r.Context(), username, id, mapID); err != nil {
				writeStoreError(w, err, store.ErrAPIKeyNotFound, http.StatusNotFound, "api key not found", "failed to delete api key scope")
				return
			}

			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}
