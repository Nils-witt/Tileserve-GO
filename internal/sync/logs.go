package sync

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	"nilswitt.dev/tileserve-go/internal/store"
)

// maxLogEntriesPerRemote bounds how many recent log lines LogStore retains
// per remote — enough for the admin UI to show useful recent activity
// without growing unboundedly for a remote that's been syncing for months.
const maxLogEntriesPerRemote = 200

// LogStore is an in-memory, per-remote ring buffer of recent sync activity,
// backing the admin UI's log view (see Manager.Logs). It complements rather
// than replaces the process's own stdout logging — every entry recorded
// here is also written via the standard "log" package — so entries are lost
// on restart, same as any other line written to stdout.
type LogStore struct {
	mu      sync.Mutex
	entries map[uuid.UUID][]store.SyncLogEntry
}

func newLogStore() *LogStore {
	return &LogStore{entries: make(map[uuid.UUID][]store.SyncLogEntry)}
}

// logf formats an informational message, writes it to the process log, and
// appends it to remoteID's buffer, trimming the oldest entry once
// maxLogEntriesPerRemote is exceeded.
func (s *LogStore) logf(remoteID uuid.UUID, format string, args ...any) {
	s.record(remoteID, "info", fmt.Sprintf(format, args...))
}

// errf formats an error message, writes it to the process log prefixed so
// it's easy to grep for, and appends it to remoteID's buffer with
// Level "error" so the admin UI can call it out. Every error a sync pass
// encounters — not just the one that ultimately fails a pass — should be
// reported through this method, since map syncs run concurrently and an
// errgroup only ever surfaces the first of several concurrent failures to
// the caller; recording each one here at the point it occurs is what makes
// the rest observable.
func (s *LogStore) errf(remoteID uuid.UUID, format string, args ...any) {
	s.record(remoteID, "error", "ERROR: "+fmt.Sprintf(format, args...))
}

// record writes msg to the process log and appends it to remoteID's buffer
// with the given level, trimming the oldest entry once
// maxLogEntriesPerRemote is exceeded.
func (s *LogStore) record(remoteID uuid.UUID, level, msg string) {
	log.Print(msg)

	s.mu.Lock()
	defer s.mu.Unlock()

	existing := s.entries[remoteID]
	entries := make([]store.SyncLogEntry, len(existing), len(existing)+1)
	copy(entries, existing)
	entries = append(entries, store.SyncLogEntry{Time: time.Now(), Level: level, Message: msg})

	if len(entries) > maxLogEntriesPerRemote {
		entries = entries[len(entries)-maxLogEntriesPerRemote:]
	}

	s.entries[remoteID] = entries
}

// Logs returns remoteID's recent log entries, oldest first. It never
// returns nil, so a remote with no recorded activity yet JSON-encodes to []
// rather than null.
func (s *LogStore) Logs(remoteID uuid.UUID) []store.SyncLogEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries := s.entries[remoteID]
	out := make([]store.SyncLogEntry, len(entries))
	copy(out, entries)

	return out
}
