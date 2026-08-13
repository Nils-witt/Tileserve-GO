package sync

import (
	"context"
	"errors"
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
}

// Manager starts, stops, and restarts one background sync worker
// (see runRemote) per enabled sync_remotes row, keeping them in sync with
// the database via a periodic reconciler loop (see Start). The zero value
// is not usable; construct via NewManager.
type Manager struct {
	st       *store.Store
	dataRoot string

	mu      sync.Mutex
	workers map[uuid.UUID]*runningWorker
}

// NewManager returns a Manager for dataRoot, backed by st.
func NewManager(st *store.Store, dataRoot string) *Manager {
	return &Manager{
		st:       st,
		dataRoot: dataRoot,
		workers:  make(map[uuid.UUID]*runningWorker),
	}
}

// Start runs the reconciler loop until ctx is canceled, blocking — call it
// in its own goroutine. On return (ctx canceled), every worker it started
// has already been stopped via Stop-equivalent cleanup.
func (m *Manager) Start(ctx context.Context) {
	m.reconcile(ctx)

	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.Stop()
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
		log.Printf("sync manager: list remotes: %v", err)
		return
	}

	desired := make(map[uuid.UUID]store.SyncRemote, len(remotes))

	for _, r := range remotes {
		if r.Enabled {
			desired[r.ID] = r
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for id, w := range m.workers {
		r, ok := desired[id]
		if !ok || configChanged(w.config, r) {
			w.cancel()
			delete(m.workers, id)
		}
	}

	for id, r := range desired {
		if _, ok := m.workers[id]; ok {
			continue
		}

		m.startWorkerLocked(ctx, r)
	}
}

// configChanged reports whether a running worker needs to be restarted
// because its connection details or poll interval changed.
func configChanged(running, current store.SyncRemote) bool {
	return running.BaseURL != current.BaseURL ||
		running.RemoteAPIKeyID != current.RemoteAPIKeyID ||
		running.PrivateKeyPEM != current.PrivateKeyPEM ||
		running.PollIntervalSec != current.PollIntervalSec
}

// startWorkerLocked starts a worker goroutine for remote. Callers must hold m.mu.
func (m *Manager) startWorkerLocked(ctx context.Context, remote store.SyncRemote) {
	workerCtx, cancel := context.WithCancel(ctx)
	trigger := make(chan struct{}, 1)

	m.workers[remote.ID] = &runningWorker{cancel: cancel, trigger: trigger, config: remote}

	go runRemote(workerCtx, m.st, m.dataRoot, remote, trigger)
}

// Stop cancels every running worker. It does not wait for them to exit —
// each worker's in-flight step (at most one archive download/extraction) is
// designed to be safely abandoned and resumed on next start, per
// pullVersion's crash-safe ordering.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, w := range m.workers {
		w.cancel()
		delete(m.workers, id)
	}
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
	default:
		// A trigger is already pending; no need to queue another.
	}

	return nil
}
