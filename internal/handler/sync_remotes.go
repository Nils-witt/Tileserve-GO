package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"nilswitt.dev/tileserve-go/internal/store"
)

// syncManager is what SyncRemoteItemHandler needs from the running
// sync.Manager: triggering a manual sync, and reading back a remote's recent
// activity log. Declared as its own interface (rather than depending on
// package sync directly) so internal/handler doesn't need to import
// internal/sync, which itself imports internal/store — keeping the
// dependency direction one-way. Logs returns store.SyncLogEntry (rather than
// a type of its own) for the same reason: that type already lives in
// package store, which both internal/handler and internal/sync depend on.
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

			sr, err := st.CreateSyncRemote(r.Context(), req.Name, req.BaseURL, remoteAPIKeyID, req.PollIntervalSec, req.Enabled, req.SyncAllMaps, req.SyncNewMaps, req.SyncGeoObjects, usernameFromContext(r.Context()))
			if err != nil {
				http.Error(w, "failed to create sync remote", http.StatusInternalServerError)
				return
			}

			if err := st.SetSyncRemoteSelectedMaps(r.Context(), sr.ID, selectedMapIDs); err != nil {
				http.Error(w, "failed to save selected maps", http.StatusInternalServerError)
				return
			}

			recordAudit(r, st, "create", "sync_remote", sr.ID.String(), fmt.Sprintf("name=%q baseUrl=%q", sr.Name, sr.BaseURL))

			writeJSON(w, http.StatusCreated, sr)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
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

// SyncRemoteItemHandler serves
// /sync/remotes/{id}[/trigger|/logs|/remote-maps|/selected-maps]
// (admin-only): GET fetches, PUT updates, DELETE removes a sync remote;
// POST .../trigger asks mgr to run an immediate sync for it, outside its
// poll interval; GET .../logs returns its recent in-memory activity log;
// GET .../remote-maps proxies a live map listing from the remote instance
// itself, for the selective-sync map picker; GET .../selected-maps returns
// the admin's already-saved selection.
func SyncRemoteItemHandler(st *store.Store, mgr syncManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r, st) {
			return
		}

		id, rest, ok := parseSyncRemoteItemPath(w, r)
		if !ok {
			return
		}

		switch rest {
		case "trigger":
			triggerSyncRemote(w, r, st, mgr, id)
			return
		case "logs":
			getSyncRemoteLogs(w, r, mgr, id)
			return
		case "remote-maps":
			getSyncRemoteRemoteMaps(w, r, mgr, id)
			return
		case "selected-maps":
			getSyncRemoteSelectedMaps(w, r, st, id)
			return
		case "":
			// falls through to the collection-item switch below
		default:
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

	sr, err := st.UpdateSyncRemote(r.Context(), id, req.Name, req.BaseURL, remoteAPIKeyID, req.PollIntervalSec, req.Enabled, req.SyncAllMaps, req.SyncNewMaps, req.SyncGeoObjects, usernameFromContext(r.Context()))
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

	recordAudit(r, st, "update", "sync_remote", sr.ID.String(), fmt.Sprintf("name=%q baseUrl=%q enabled=%v", sr.Name, sr.BaseURL, sr.Enabled))

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

	recordAudit(r, st, "delete", "sync_remote", id.String(), "")

	w.WriteHeader(http.StatusNoContent)
}

func triggerSyncRemote(w http.ResponseWriter, r *http.Request, st *store.Store, mgr syncManager, id uuid.UUID) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	if err := mgr.Trigger(id); err != nil {
		http.Error(w, "sync remote is not currently running (check it exists and is enabled)", http.StatusConflict)
		return
	}

	recordAudit(r, st, "trigger", "sync_remote", id.String(), "")

	w.WriteHeader(http.StatusAccepted)
}

// getSyncRemoteLogs returns id's recent in-memory sync activity log, oldest
// first. It doesn't check whether id names an existing remote — an unknown
// or never-synced id simply has no entries yet, same as a freshly created
// one, so there's nothing useful a 404 would add here.
func getSyncRemoteLogs(w http.ResponseWriter, r *http.Request, mgr syncManager, id uuid.UUID) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	writeJSON(w, http.StatusOK, mgr.Logs(id))
}

// getSyncRemoteRemoteMaps proxies a live GET .../maps call to id's remote
// instance, for the admin UI's selective-sync map picker — distinct from
// getSyncRemoteSelectedMaps, which returns what's already been chosen to
// sync, not what's available to choose from. Failure reaching the remote is
// reported as a 502, since it reflects the remote's availability, not this
// server's.
func getSyncRemoteRemoteMaps(w http.ResponseWriter, r *http.Request, mgr syncManager, id uuid.UUID) {
	if !requireMethod(w, r, http.MethodGet) {
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

	writeJSON(w, http.StatusOK, maps)
}

// getSyncRemoteSelectedMaps returns id's saved explicit map selection
// (used when its sync_all_maps is false), for the admin UI to pre-check the
// right boxes in the selective-sync map picker.
func getSyncRemoteSelectedMaps(w http.ResponseWriter, r *http.Request, st *store.Store, id uuid.UUID) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	ids, err := st.ListSyncRemoteSelectedMaps(r.Context(), id)
	if err != nil {
		http.Error(w, "failed to list selected maps", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, ids)
}
