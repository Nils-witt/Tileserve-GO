package handler

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"nilswitt.dev/tileserve-go/internal/store"
)

// syncTrigger lets a sync remote's manual trigger route ask the running
// sync.Manager to kick off an immediate sync, outside its poll interval.
// Declared as its own interface (rather than depending on package sync
// directly) so internal/handler doesn't need to import internal/sync, which
// itself imports internal/store — keeping the dependency direction one-way.
type syncTrigger interface {
	Trigger(id uuid.UUID) error
}

type syncRemoteRequest struct {
	Name            string `json:"name"`
	BaseURL         string `json:"baseUrl"`
	APIKey          string `json:"apiKey"`
	PollIntervalSec int    `json:"pollIntervalSec"`
	Enabled         bool   `json:"enabled"`
}

// SyncRemotesCollectionHandler serves the /sync/remotes collection route
// (admin-only): GET lists configured remotes, POST registers a new one.
func SyncRemotesCollectionHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r, st) {
			return
		}

		switch r.Method {
		case http.MethodGet:
			remotes, err := st.ListSyncRemotes(r.Context())
			if err != nil {
				http.Error(w, "failed to list sync remotes", http.StatusInternalServerError)
				return
			}

			writeJSON(w, http.StatusOK, remotes)

		case http.MethodPost:
			var req syncRemoteRequest
			if !decodeJSON(w, r, &req) {
				return
			}

			if !validateSyncRemoteRequest(w, req) {
				return
			}

			sr, err := st.CreateSyncRemote(r.Context(), req.Name, req.BaseURL, req.APIKey, req.PollIntervalSec, req.Enabled, usernameFromContext(r.Context()))
			if err != nil {
				http.Error(w, "failed to create sync remote", http.StatusInternalServerError)
				return
			}

			writeJSON(w, http.StatusCreated, sr)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// validateSyncRemoteRequest checks req's required fields, writing a 400 and
// returning false if invalid.
func validateSyncRemoteRequest(w http.ResponseWriter, req syncRemoteRequest) bool {
	if req.Name == "" || req.BaseURL == "" || req.APIKey == "" {
		http.Error(w, "name, baseUrl, and apiKey are required", http.StatusBadRequest)
		return false
	}

	if req.PollIntervalSec <= 0 {
		http.Error(w, "pollIntervalSec must be positive", http.StatusBadRequest)
		return false
	}

	return true
}

// SyncRemoteItemHandler serves /sync/remotes/{id}[/trigger] (admin-only):
// GET fetches, PUT updates, DELETE removes a sync remote; POST .../trigger
// asks mgr to run an immediate sync for it, outside its poll interval.
func SyncRemoteItemHandler(st *store.Store, mgr syncTrigger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r, st) {
			return
		}

		id, rest, ok := parseSyncRemoteItemPath(w, r)
		if !ok {
			return
		}

		if rest == "trigger" {
			triggerSyncRemote(w, r, mgr, id)
			return
		}

		if rest != "" {
			http.NotFound(w, r)
			return
		}

		switch r.Method {
		case http.MethodGet:
			getSyncRemote(w, r, st, id)
		case http.MethodPut:
			updateSyncRemote(w, r, st, id)
		case http.MethodDelete:
			deleteSyncRemote(w, r, st, id)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// parseSyncRemoteItemPath splits /sync/remotes/{id}[/{rest}] into id and the
// optional trailing segment, writing a 400 and returning ok=false if id
// isn't a valid UUID.
func parseSyncRemoteItemPath(w http.ResponseWriter, r *http.Request) (id uuid.UUID, rest string, ok bool) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/sync/remotes/"), "/")
	segments := strings.SplitN(path, "/", 2)

	id, err := uuid.Parse(segments[0])
	if err != nil {
		http.Error(w, "invalid sync remote id", http.StatusBadRequest)
		return uuid.UUID{}, "", false
	}

	if len(segments) == 2 {
		rest = segments[1]
	}

	return id, rest, true
}

func getSyncRemote(w http.ResponseWriter, r *http.Request, st *store.Store, id uuid.UUID) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	sr, err := st.GetSyncRemote(r.Context(), id)
	if err != nil {
		writeStoreError(w, err, store.ErrSyncRemoteNotFound, http.StatusNotFound, "sync remote not found", "failed to get sync remote")
		return
	}

	writeJSON(w, http.StatusOK, sr)
}

func updateSyncRemote(w http.ResponseWriter, r *http.Request, st *store.Store, id uuid.UUID) {
	var req syncRemoteRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if !validateSyncRemoteRequest(w, req) {
		return
	}

	sr, err := st.UpdateSyncRemote(r.Context(), id, req.Name, req.BaseURL, req.APIKey, req.PollIntervalSec, req.Enabled, usernameFromContext(r.Context()))
	if err != nil {
		writeStoreError(w, err, store.ErrSyncRemoteNotFound, http.StatusNotFound, "sync remote not found", "failed to update sync remote")
		return
	}

	writeJSON(w, http.StatusOK, sr)
}

func deleteSyncRemote(w http.ResponseWriter, r *http.Request, st *store.Store, id uuid.UUID) {
	if !requireMethod(w, r, http.MethodDelete) {
		return
	}

	if err := st.DeleteSyncRemote(r.Context(), id); err != nil {
		writeStoreError(w, err, store.ErrSyncRemoteNotFound, http.StatusNotFound, "sync remote not found", "failed to delete sync remote")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func triggerSyncRemote(w http.ResponseWriter, r *http.Request, mgr syncTrigger, id uuid.UUID) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	if err := mgr.Trigger(id); err != nil {
		http.Error(w, "sync remote is not currently running (check it exists and is enabled)", http.StatusConflict)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}
