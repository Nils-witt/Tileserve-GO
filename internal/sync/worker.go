package sync

import (
	"context"
	"log"
	"time"

	"nilswitt.dev/tileserve-go/internal/store"
)

// defaultPollInterval is used if a remote's configured poll interval is
// non-positive (shouldn't happen — the admin API validates it — but a
// worker must never busy-loop on a zero-duration ticker).
const defaultPollInterval = 5 * time.Minute

// runRemote runs remote's periodic sync loop until ctx is canceled: an
// immediate sync on start, then one every remote.PollIntervalSec, or
// whenever a signal arrives on trigger (see Manager.Trigger). Each run's
// outcome is persisted via st.SetSyncRemoteStatus for observability. Meant
// to be launched in its own goroutine by Manager.
func runRemote(ctx context.Context, st *store.Store, dataRoot string, remote store.SyncRemote, trigger <-chan struct{}) {
	runOnce(ctx, st, dataRoot, remote)

	interval := time.Duration(remote.PollIntervalSec) * time.Second
	if interval <= 0 {
		interval = defaultPollInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOnce(ctx, st, dataRoot, remote)
		case <-trigger:
			runOnce(ctx, st, dataRoot, remote)
		}
	}
}

// runOnce performs a single sync pass for remote and records its outcome.
// Errors are logged, not propagated — a failed sync tick is retried on the
// next tick/trigger, not treated as fatal to the worker loop. The client is
// (re)built on every call, rather than once per worker goroutine, because
// NewClient can now fail (a malformed stored private key) — building it
// here means that failure surfaces as a normal recurring sync-status error
// instead of permanently stranding the worker.
func runOnce(ctx context.Context, st *store.Store, dataRoot string, remote store.SyncRemote) {
	status, errMsg := "ok", ""

	client, err := NewClient(remote.BaseURL, remote.RemoteAPIKeyID, remote.PrivateKeyPEM)
	if err != nil {
		status, errMsg = "error", err.Error()
		log.Printf("sync remote %s (%s): %v", remote.Name, remote.ID, err)
	} else if err := syncRemoteOnce(ctx, st, dataRoot, client, remote); err != nil {
		status, errMsg = "error", err.Error()
		log.Printf("sync remote %s (%s): %v", remote.Name, remote.ID, err)
	}

	if err := st.SetSyncRemoteStatus(ctx, remote.ID, status, errMsg, time.Now()); err != nil {
		log.Printf("sync remote %s (%s): record status: %v", remote.Name, remote.ID, err)
	}
}
