package sync

import (
	"context"
	"crypto/rsa"
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
// outcome is persisted via st.SetSyncRemoteStatus for observability, and its
// log lines are recorded in logs for the admin UI's log view (see
// Manager.Logs). Meant to be launched in its own goroutine by Manager.
func runRemote(ctx context.Context, st *store.Store, dataRoot string, remote store.SyncRemote, privateKey *rsa.PrivateKey, trigger <-chan struct{}, logs *LogStore) {
	interval := time.Duration(remote.PollIntervalSec) * time.Second
	if interval <= 0 {
		interval = defaultPollInterval
	}

	logs.logf(remote.ID, "sync remote %s (%s): worker started, polling every %s", remote.Name, remote.ID, interval)

	runOnce(ctx, st, dataRoot, remote, privateKey, logs)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logs.logf(remote.ID, "sync remote %s (%s): worker stopped", remote.Name, remote.ID)
			return
		case <-ticker.C:
			runOnce(ctx, st, dataRoot, remote, privateKey, logs)
		case <-trigger:
			logs.logf(remote.ID, "sync remote %s (%s): running triggered sync", remote.Name, remote.ID)
			runOnce(ctx, st, dataRoot, remote, privateKey, logs)
		}
	}
}

// runOnce performs a single sync pass for remote and records its outcome.
// Errors are logged, not propagated — a failed sync tick is retried on the
// next tick/trigger, not treated as fatal to the worker loop.
func runOnce(ctx context.Context, st *store.Store, dataRoot string, remote store.SyncRemote, privateKey *rsa.PrivateKey, logs *LogStore) {
	start := time.Now()
	status, errMsg := "ok", ""

	logs.logf(remote.ID, "sync remote %s (%s): starting sync pass", remote.Name, remote.ID)

	client := NewClient(remote.BaseURL, remote.RemoteAPIKeyID, privateKey)
	if err := syncRemoteOnce(ctx, st, dataRoot, client, remote, logs); err != nil {
		status, errMsg = "error", err.Error()
		logs.errf(remote.ID, "sync remote %s (%s): sync pass failed: %v", remote.Name, remote.ID, err)
	}

	if status == "ok" {
		logs.logf(remote.ID, "sync remote %s (%s): sync pass complete in %s", remote.Name, remote.ID, time.Since(start).Round(time.Millisecond))
	} else {
		logs.errf(remote.ID, "sync remote %s (%s): sync pass failed after %s", remote.Name, remote.ID, time.Since(start).Round(time.Millisecond))
	}

	if err := st.SetSyncRemoteStatus(ctx, remote.ID, status, errMsg, time.Now()); err != nil {
		logs.errf(remote.ID, "sync remote %s (%s): record status: %v", remote.Name, remote.ID, err)
	}
}
