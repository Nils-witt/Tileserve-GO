package sync

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	"nilswitt.dev/tileserve-go/internal/store"
)

// reconcileInterval is how often Manager re-reads sync_remotes and
// starts/stops/restarts worker goroutines to match. There's no
// config-change push mechanism to build on, so a small poll-and-diff
// reconciler is the simplest correct approach.
const reconcileInterval = 30 * time.Second

// ErrRemoteNotRunning is returned by Trigger when id has no active worker
// (it doesn't exist, or is disabled).
var ErrRemoteNotRunning = errors.New("sync remote is not currently running")

// runningWorker tracks one remote's active sync goroutine so Manager can
// stop or restart it when its configuration changes.
type runningWorker struct {
	cancel  context.CancelFunc
	trigger chan struct{}
	config  store.SyncRemote
	done    chan struct{} // closed when the worker goroutine returns
}

// Manager starts, stops, and restarts one background sync worker
// (see runRemote) per enabled sync_remotes row, keeping them in sync with
// the database via a periodic reconciler loop (see Start). The zero value
// is not usable; construct via NewManager.
type Manager struct {
	st       *store.Store
	dataRoot string
	logs     *LogStore

	mu      sync.Mutex
	workers map[uuid.UUID]*runningWorker
}

// NewManager returns a Manager for dataRoot, backed by st.
func NewManager(st *store.Store, dataRoot string) *Manager {
	return &Manager{
		st:       st,
		dataRoot: dataRoot,
		logs:     newLogStore(),
		workers:  make(map[uuid.UUID]*runningWorker),
	}
}

// Logs returns id's recent sync activity log, oldest first, for the admin
// UI's log view. An id with no worker (never existed, or never run) simply
// has no entries yet.
func (m *Manager) Logs(id uuid.UUID) []store.SyncLogEntry {
	return m.logs.Logs(id)
}

// Start runs the reconciler loop until ctx is canceled, blocking — call it
// in its own goroutine. On return (ctx canceled), every worker it started
// has already been stopped via Stop-equivalent cleanup.
func (m *Manager) Start(ctx context.Context) {
	log.Printf("sync manager: starting, reconciling every %s", reconcileInterval)

	m.reconcile(ctx)

	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.Stop()
			log.Printf("sync manager: stopped")

			return
		case <-ticker.C:
			m.reconcile(ctx)
		}
	}
}

// reconcile re-reads sync_remotes and starts/stops/restarts worker
// goroutines to match: a new enabled remote starts, a removed or disabled
// one stops, and one whose connection details or interval changed restarts
// with the new configuration.
func (m *Manager) reconcile(ctx context.Context) {
	remotes, err := m.st.ListSyncRemotes(ctx)
	if err != nil {
		log.Printf("sync manager: ERROR: list remotes: %v", err)
		return
	}

	desired := make(map[uuid.UUID]store.SyncRemote, len(remotes))

	for _, r := range remotes {
		if r.Enabled {
			desired[r.ID] = r
		}
	}

	m.mu.Lock()

	var stopped []*runningWorker

	restart := make(map[uuid.UUID]store.SyncRemote)

	for id, w := range m.workers {
		r, ok := desired[id]
		switch {
		case !ok:
			m.logs.logf(id, "sync manager: stopping worker for remote %s (%s): removed or disabled", w.config.Name, id)
			w.cancel()
			stopped = append(stopped, w)

			delete(m.workers, id)
		case configChanged(w.config, r):
			m.logs.logf(id, "sync manager: restarting worker for remote %s (%s): configuration changed", w.config.Name, id)
			w.cancel()
			stopped = append(stopped, w)
			restart[id] = r

			delete(m.workers, id)
		}
	}

	m.mu.Unlock()

	// Wait for every canceled worker to fully exit before starting its
	// replacement. cancel() only takes effect the next time the worker
	// checks its context, so without this a restarted remote could briefly
	// have its old and new workers both mid-sync, racing on the same
	// on-disk map directories.
	for _, w := range stopped {
		<-w.done
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for id, r := range restart {
		m.logs.logf(id, "sync manager: starting worker for remote %s (%s)", r.Name, id)
		m.startWorkerLocked(ctx, r)
	}

	for id, r := range desired {
		if _, ok := m.workers[id]; ok {
			continue
		}

		m.logs.logf(id, "sync manager: starting worker for remote %s (%s)", r.Name, id)
		m.startWorkerLocked(ctx, r)
	}
}

// configChanged reports whether a running worker needs to be restarted
// because its connection details, poll interval, or selective-sync policy
// changed. The map selection itself (sync_remote_maps) doesn't need to be
// listed here — it's read fresh from the store on every sync pass (see
// mapsForRemote), not captured in the worker's remote snapshot.
func configChanged(running, current store.SyncRemote) bool {
	return running.BaseURL != current.BaseURL ||
		running.RemoteAPIKeyID != current.RemoteAPIKeyID ||
		running.PrivateKeyPEM != current.PrivateKeyPEM ||
		running.PollIntervalSec != current.PollIntervalSec ||
		running.SyncAllMaps != current.SyncAllMaps ||
		running.SyncNewMaps != current.SyncNewMaps ||
		running.SyncGeoObjects != current.SyncGeoObjects
}

// startWorkerLocked starts a worker goroutine for remote. Callers must hold m.mu.
func (m *Manager) startWorkerLocked(ctx context.Context, remote store.SyncRemote) {
	workerCtx, cancel := context.WithCancel(ctx)
	trigger := make(chan struct{}, 1)
	done := make(chan struct{})

	m.workers[remote.ID] = &runningWorker{cancel: cancel, trigger: trigger, config: remote, done: done}

	go func() {
		defer close(done)

		runRemote(workerCtx, m.st, m.dataRoot, remote, trigger, m.logs)
	}()
}

// Stop cancels every running worker. It does not wait for them to exit —
// each worker's in-flight step (at most one archive download/extraction) is
// designed to be safely abandoned and resumed on next start, per
// pullVersion's crash-safe ordering.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.workers) > 0 {
		log.Printf("sync manager: stopping %d running worker(s)", len(m.workers))
	}

	for id, w := range m.workers {
		w.cancel()
		delete(m.workers, id)
	}
}

// ListRemoteMaps fetches the live list of maps visible to id's configured
// API key on its remote instance, for the admin UI's selective-sync map
// picker. It builds a short-lived Client directly from the stored
// configuration rather than reusing a running worker, so it works
// regardless of whether id currently has one (e.g. a disabled remote, or
// one between poll ticks).
func (m *Manager) ListRemoteMaps(ctx context.Context, id uuid.UUID) ([]store.MapRecord, error) {
	remote, err := m.st.GetSyncRemote(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get sync remote: %w", err)
	}

	client, err := NewClient(remote.BaseURL, remote.RemoteAPIKeyID, remote.PrivateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("build client: %w", err)
	}

	maps, err := client.ListMaps(ctx)
	if err != nil {
		return nil, fmt.Errorf("list remote maps: %w", err)
	}

	return maps, nil
}

// Trigger asks the running worker for id to start an immediate sync,
// outside its regular poll interval. It returns ErrRemoteNotRunning if no
// worker is currently running for id (it doesn't exist, or is disabled).
func (m *Manager) Trigger(id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	w, ok := m.workers[id]
	if !ok {
		return ErrRemoteNotRunning
	}

	select {
	case w.trigger <- struct{}{}:
		m.logs.logf(id, "sync manager: triggered immediate sync for remote %s (%s)", w.config.Name, id)
	default:
		// A trigger is already pending; no need to queue another.
		m.logs.logf(id, "sync manager: trigger for remote %s (%s) already pending, ignoring", w.config.Name, id)
	}

	return nil
}
