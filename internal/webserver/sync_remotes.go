package webserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"nilswitt.dev/tileserve-go/internal/auth"
	"nilswitt.dev/tileserve-go/internal/httputil"
	"nilswitt.dev/tileserve-go/internal/webserver/auditlog"

	"nilswitt.dev/tileserve-go/internal/store"
)

// syncManager is what SyncRemoteItemHandler needs from the running
// sync.Manager: triggering a manual sync, and reading back a remote's recent
// activity log. Declared as its own interface (rather than depending on
// package sync directly) so internal/webserver doesn't need to import
// internal/sync, which itself imports internal/store — keeping the
// dependency direction one-way. Logs returns store.SyncLogEntry (rather than
// a type of its own) for the same reason: that type already lives in
// package store, which both internal/webserver and internal/sync depend on.
type syncManager interface {
	Trigger(id uuid.UUID) error
	Logs(id uuid.UUID) []store.SyncLogEntry
	ListRemoteMaps(ctx context.Context, id uuid.UUID) ([]store.MapRecord, error)
}

type syncRemoteRequest struct {
	Name            string `json:"name"`
	BaseURL         string `json:"baseUrl"`
	RemoteAPIKeyID  string `json:"remoteApiKeyId"`
	PollIntervalSec int    `json:"pollIntervalSec"`
	Enabled         bool   `json:"enabled"`
	SyncAllMaps     bool   `json:"syncAllMaps"`
	SyncNewMaps     bool   `json:"syncNewMaps"`
	SyncGeoObjects  bool   `json:"syncGeoObjects"`
	// SelectedMapIDs is a pointer so a request that omits it (e.g. a PUT
	// that only means to toggle `enabled`) leaves the saved selection
	// untouched, distinct from one that explicitly sends an empty list to
	// clear it — a plain []string can't tell those two apart, since both
	// decode to a nil slice.
	SelectedMapIDs *[]string `json:"selectedMapUuids,omitempty"`
}

// SyncRemotesListHandler serves GET /sync/remotes (admin-only): lists
// configured remotes.
func SyncRemotesListHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		remotes, err := st.ListSyncRemotes(r.Context())
		if err != nil {
			http.Error(w, "failed to list sync remotes", http.StatusInternalServerError)
			return
		}

		httputil.WriteJSON(w, http.StatusOK, remotes)
	}
}

// SyncRemoteCreateHandler serves POST /sync/remotes (admin-only): registers
// a new sync remote.
func SyncRemoteCreateHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req syncRemoteRequest
		if !httputil.DecodeJSON(w, r, &req) {
			return
		}

		remoteAPIKeyID, ok := validateSyncRemoteRequest(w, req)
		if !ok {
			return
		}

		var rawSelection []string
		if req.SelectedMapIDs != nil {
			rawSelection = *req.SelectedMapIDs
		}

		selectedMapIDs, ok := parseUUIDList(w, rawSelection)
		if !ok {
			return
		}

		sr, err := st.CreateSyncRemote(r.Context(), req.Name, req.BaseURL, remoteAPIKeyID, req.PollIntervalSec, req.Enabled, req.SyncAllMaps, req.SyncNewMaps, req.SyncGeoObjects, auth.UsernameFromContext(r.Context()))
		if err != nil {
			http.Error(w, "failed to create sync remote", http.StatusInternalServerError)
			return
		}

		if err := st.SetSyncRemoteSelectedMaps(r.Context(), sr.ID, selectedMapIDs); err != nil {
			http.Error(w, "failed to save selected maps", http.StatusInternalServerError)
			return
		}

		auditlog.RecordAudit(r, st, "create", "sync_remote", sr.ID.String(), fmt.Sprintf("name=%q baseUrl=%q", sr.Name, sr.BaseURL))

		httputil.WriteJSON(w, http.StatusCreated, sr)
	}
}

// validateSyncRemoteRequest checks req's required fields, writing a 400 and
// returning ok=false if invalid, along with the parsed remoteApiKeyId on
// success. remoteApiKeyId is always required — it isn't secret, so GET/list
// responses always echo it back for the UI to resubmit.
func validateSyncRemoteRequest(w http.ResponseWriter, req syncRemoteRequest) (uuid.UUID, bool) {
	if req.Name == "" || req.BaseURL == "" || req.RemoteAPIKeyID == "" {
		http.Error(w, "name, baseUrl, and remoteApiKeyId are required", http.StatusBadRequest)
		return uuid.UUID{}, false
	}

	remoteAPIKeyID, err := uuid.Parse(req.RemoteAPIKeyID)
	if err != nil {
		http.Error(w, "remoteApiKeyId must be a valid uuid", http.StatusBadRequest)
		return uuid.UUID{}, false
	}

	if req.PollIntervalSec <= 0 {
		http.Error(w, "pollIntervalSec must be positive", http.StatusBadRequest)
		return uuid.UUID{}, false
	}

	return remoteAPIKeyID, true
}

// parseUUIDList parses raw as a list of UUID strings, writing a 400 and
// returning ok=false on the first invalid one. Used for
// syncRemoteRequest.SelectedMapIDs, the admin's explicit selective-sync map
// selection.
func parseUUIDList(w http.ResponseWriter, raw []string) ([]uuid.UUID, bool) {
	ids := make([]uuid.UUID, 0, len(raw))

	for _, s := range raw {
		id, err := uuid.Parse(s)
		if err != nil {
			http.Error(w, "selectedMapUuids must be valid uuids", http.StatusBadRequest)
			return nil, false
		}

		ids = append(ids, id)
	}

	return ids, true
}

// SyncRemoteGetHandler serves GET /sync/remotes/{id} (admin-only): fetches
// a single sync remote.
func SyncRemoteGetHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httputil.PathUUID(w, r, "id", "sync remote id")
		if !ok {
			return
		}

		sr, err := st.GetSyncRemote(r.Context(), id)
		if err != nil {
			writeStoreError(w, err, store.ErrSyncRemoteNotFound, http.StatusNotFound, "sync remote not found", "failed to get sync remote")
			return
		}

		httputil.WriteJSON(w, http.StatusOK, sr)
	}
}

// SyncRemoteUpdateHandler serves PUT /sync/remotes/{id} (admin-only):
// updates a sync remote.
func SyncRemoteUpdateHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httputil.PathUUID(w, r, "id", "sync remote id")
		if !ok {
			return
		}

		var req syncRemoteRequest
		if !httputil.DecodeJSON(w, r, &req) {
			return
		}

		remoteAPIKeyID, ok := validateSyncRemoteRequest(w, req)
		if !ok {
			return
		}

		var selectedMapIDs []uuid.UUID

		if req.SelectedMapIDs != nil {
			var ok bool

			selectedMapIDs, ok = parseUUIDList(w, *req.SelectedMapIDs)
			if !ok {
				return
			}
		}

		sr, err := st.UpdateSyncRemote(r.Context(), id, req.Name, req.BaseURL, remoteAPIKeyID, req.PollIntervalSec, req.Enabled, req.SyncAllMaps, req.SyncNewMaps, req.SyncGeoObjects, auth.UsernameFromContext(r.Context()))
		if err != nil {
			writeStoreError(w, err, store.ErrSyncRemoteNotFound, http.StatusNotFound, "sync remote not found", "failed to update sync remote")
			return
		}

		// req.SelectedMapIDs == nil means the caller didn't intend to touch the
		// selection (e.g. a PUT that only flips `enabled`) — see its doc
		// comment — so the saved one is left as-is rather than being cleared.
		if req.SelectedMapIDs != nil {
			if err := st.SetSyncRemoteSelectedMaps(r.Context(), sr.ID, selectedMapIDs); err != nil {
				http.Error(w, "failed to save selected maps", http.StatusInternalServerError)
				return
			}
		}

		auditlog.RecordAudit(r, st, "update", "sync_remote", sr.ID.String(), fmt.Sprintf("name=%q baseUrl=%q enabled=%v", sr.Name, sr.BaseURL, sr.Enabled))

		httputil.WriteJSON(w, http.StatusOK, sr)
	}
}

// SyncRemoteDeleteHandler serves DELETE /sync/remotes/{id} (admin-only):
// removes a sync remote.
//
//nolint:dupl // structurally the same simple "delete by id" handler as GroupDeleteHandler; each entity's own delete endpoint follows this same idiom throughout the package, and a shared generic helper isn't worth the indirection for a handful of lines
func SyncRemoteDeleteHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httputil.PathUUID(w, r, "id", "sync remote id")
		if !ok {
			return
		}

		if err := st.DeleteSyncRemote(r.Context(), id); err != nil {
			writeStoreError(w, err, store.ErrSyncRemoteNotFound, http.StatusNotFound, "sync remote not found", "failed to delete sync remote")
			return
		}

		auditlog.RecordAudit(r, st, "delete", "sync_remote", id.String(), "")

		w.WriteHeader(http.StatusNoContent)
	}
}

// SyncRemoteTriggerHandler serves POST /sync/remotes/{id}/trigger
// (admin-only): asks mgr to run an immediate sync for id, outside its poll
// interval.
func SyncRemoteTriggerHandler(st *store.Store, mgr syncManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httputil.PathUUID(w, r, "id", "sync remote id")
		if !ok {
			return
		}

		if err := mgr.Trigger(id); err != nil {
			http.Error(w, "sync remote is not currently running (check it exists and is enabled)", http.StatusConflict)
			return
		}

		auditlog.RecordAudit(r, st, "trigger", "sync_remote", id.String(), "")

		w.WriteHeader(http.StatusAccepted)
	}
}

// SyncRemoteLogsHandler serves GET /sync/remotes/{id}/logs (admin-only):
// returns id's recent in-memory sync activity log, oldest first. It doesn't
// check whether id names an existing remote — an unknown or never-synced id
// simply has no entries yet, same as a freshly created one, so there's
// nothing useful a 404 would add here.
func SyncRemoteLogsHandler(mgr syncManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httputil.PathUUID(w, r, "id", "sync remote id")
		if !ok {
			return
		}

		httputil.WriteJSON(w, http.StatusOK, mgr.Logs(id))
	}
}

// SyncRemoteRemoteMapsHandler serves GET /sync/remotes/{id}/remote-maps
// (admin-only): proxies a live GET .../maps call to id's remote instance,
// for the admin UI's selective-sync map picker — distinct from
// SyncRemoteSelectedMapsHandler, which returns what's already been chosen
// to sync, not what's available to choose from. Failure reaching the
// remote is reported as a 502, since it reflects the remote's availability,
// not this server's.
func SyncRemoteRemoteMapsHandler(mgr syncManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httputil.PathUUID(w, r, "id", "sync remote id")
		if !ok {
			return
		}

		maps, err := mgr.ListRemoteMaps(r.Context(), id)
		if err != nil {
			if errors.Is(err, store.ErrSyncRemoteNotFound) {
				http.Error(w, "sync remote not found", http.StatusNotFound)
				return
			}

			http.Error(w, "failed to list remote maps: "+err.Error(), http.StatusBadGateway)

			return
		}

		httputil.WriteJSON(w, http.StatusOK, maps)
	}
}

// SyncRemoteSelectedMapsHandler serves GET
// /sync/remotes/{id}/selected-maps (admin-only): returns id's saved explicit
// map selection (used when its sync_all_maps is false), for the admin UI to
// pre-check the right boxes in the selective-sync map picker.
func SyncRemoteSelectedMapsHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httputil.PathUUID(w, r, "id", "sync remote id")
		if !ok {
			return
		}

		ids, err := st.ListSyncRemoteSelectedMaps(r.Context(), id)
		if err != nil {
			http.Error(w, "failed to list selected maps", http.StatusInternalServerError)
			return
		}

		httputil.WriteJSON(w, http.StatusOK, ids)
	}
}
