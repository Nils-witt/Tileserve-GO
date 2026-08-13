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

// logf formats a message, writes it to the process log, and appends it to
// remoteID's buffer, trimming the oldest entry once maxLogEntriesPerRemote
// is exceeded.
func (s *LogStore) logf(remoteID uuid.UUID, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Print(msg)

	s.mu.Lock()
	defer s.mu.Unlock()

	existing := s.entries[remoteID]
	entries := make([]store.SyncLogEntry, len(existing), len(existing)+1)
	copy(entries, existing)
	entries = append(entries, store.SyncLogEntry{Time: time.Now(), Message: msg})

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
