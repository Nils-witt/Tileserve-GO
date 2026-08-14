package sync

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"nilswitt.dev/tileserve-go/internal/store"
)

func TestVersionsToPull(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	remote := []store.MapVersionRecord{
		{Version: "3", CreatedAt: base.Add(3 * time.Hour)},
		{Version: "1", CreatedAt: base.Add(1 * time.Hour)},
		{Version: "2", CreatedAt: base.Add(2 * time.Hour)},
	}

	alreadyLocal := map[string]bool{"1": true}

	got := versionsToPull(alreadyLocal, remote)

	if len(got) != 2 {
		t.Fatalf("versionsToPull() returned %d versions, want 2: %+v", len(got), got)
	}

	if got[0].Version != "2" || got[1].Version != "3" {
		t.Fatalf("versionsToPull() = %+v, want oldest-first [2, 3]", got)
	}
}

func TestVersionsToPullSkipsAllWhenFullyLocal(t *testing.T) {
	t.Parallel()

	remote := []store.MapVersionRecord{
		{Version: "1", CreatedAt: time.Now()},
		{Version: "2", CreatedAt: time.Now()},
	}

	got := versionsToPull(map[string]bool{"1": true, "2": true}, remote)

	if len(got) != 0 {
		t.Fatalf("versionsToPull() = %+v, want empty", got)
	}
}

func TestReconcileAliases(t *testing.T) {
	t.Parallel()

	local := []store.MapVersionAlias{
		{Alias: "stable", Version: "1"},
		{Alias: "beta", Version: "2"},
		{Alias: "local-only", Version: "1"},
	}

	remote := []store.MapVersionAlias{
		{Alias: "stable", Version: "1"},    // unchanged
		{Alias: "beta", Version: "3"},      // repointed
		{Alias: "new-alias", Version: "1"}, // new
	}

	toSet, toDelete := reconcileAliases(local, remote)

	if len(toSet) != 2 {
		t.Fatalf("toSet = %+v, want 2 entries (beta repointed, new-alias added)", toSet)
	}

	setByAlias := make(map[string]string, len(toSet))
	for _, a := range toSet {
		setByAlias[a.Alias] = a.Version
	}

	if setByAlias["beta"] != "3" {
		t.Fatalf("toSet[beta] = %q, want %q", setByAlias["beta"], "3")
	}

	if setByAlias["new-alias"] != "1" {
		t.Fatalf("toSet[new-alias] = %q, want %q", setByAlias["new-alias"], "1")
	}

	if _, ok := setByAlias["stable"]; ok {
		t.Fatalf("toSet should not include unchanged alias %q", "stable")
	}

	if len(toDelete) != 1 || toDelete[0] != "local-only" {
		t.Fatalf("toDelete = %+v, want [local-only]", toDelete)
	}
}

func TestReconcileAliasesEmptyBothSides(t *testing.T) {
	t.Parallel()

	toSet, toDelete := reconcileAliases(nil, nil)

	if len(toSet) != 0 || len(toDelete) != 0 {
		t.Fatalf("reconcileAliases(nil, nil) = (%+v, %+v), want (nil, nil)", toSet, toDelete)
	}
}

func TestReconcileGeoObjectsUpsertNew(t *testing.T) {
	t.Parallel()

	now := time.Now()
	remoteOnly := store.GeoObjectRecord{UUID: uuid.New(), Name: "remote-only", UpdatedAt: now}

	toUpsert, toDelete := reconcileGeoObjects(nil, []store.GeoObjectRecord{remoteOnly})

	if len(toUpsert) != 1 || toUpsert[0].UUID != remoteOnly.UUID {
		t.Fatalf("toUpsert = %+v, want [remoteOnly]", toUpsert)
	}

	if len(toDelete) != 0 {
		t.Fatalf("toDelete = %+v, want empty", toDelete)
	}
}

func TestReconcileGeoObjectsUpsertChanged(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	now := time.Now()

	local := []store.GeoObjectRecord{{UUID: id, Name: "old-name", UpdatedAt: now}}
	remote := []store.GeoObjectRecord{{UUID: id, Name: "new-name", UpdatedAt: now.Add(time.Second)}}

	toUpsert, toDelete := reconcileGeoObjects(local, remote)

	if len(toUpsert) != 1 || toUpsert[0].Name != "new-name" {
		t.Fatalf("toUpsert = %+v, want [new-name]", toUpsert)
	}

	if len(toDelete) != 0 {
		t.Fatalf("toDelete = %+v, want empty", toDelete)
	}
}

func TestReconcileGeoObjectsUnchangedSkipped(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	now := time.Now()

	local := []store.GeoObjectRecord{{UUID: id, Name: "same", UpdatedAt: now}}
	remote := []store.GeoObjectRecord{{UUID: id, Name: "same", UpdatedAt: now}}

	toUpsert, toDelete := reconcileGeoObjects(local, remote)

	if len(toUpsert) != 0 {
		t.Fatalf("toUpsert = %+v, want empty (unchanged)", toUpsert)
	}

	if len(toDelete) != 0 {
		t.Fatalf("toDelete = %+v, want empty", toDelete)
	}
}

func TestReconcileGeoObjectsDeleteStale(t *testing.T) {
	t.Parallel()

	localOnly := store.GeoObjectRecord{UUID: uuid.New(), Name: "local-only", UpdatedAt: time.Now()}

	toUpsert, toDelete := reconcileGeoObjects([]store.GeoObjectRecord{localOnly}, nil)

	if len(toUpsert) != 0 {
		t.Fatalf("toUpsert = %+v, want empty", toUpsert)
	}

	if len(toDelete) != 1 || toDelete[0] != localOnly.UUID {
		t.Fatalf("toDelete = %+v, want [%s]", toDelete, localOnly.UUID)
	}
}

func TestReconcileGeoObjectsEmptyBothSides(t *testing.T) {
	t.Parallel()

	toUpsert, toDelete := reconcileGeoObjects(nil, nil)

	if len(toUpsert) != 0 || len(toDelete) != 0 {
		t.Fatalf("reconcileGeoObjects(nil, nil) = (%+v, %+v), want (nil, nil)", toUpsert, toDelete)
	}
}

func TestMapsToSyncSelectedOnly(t *testing.T) {
	t.Parallel()

	selectedID := uuid.New()
	unselectedID := uuid.New()

	maps := []store.MapRecord{
		{UUID: selectedID, Name: "selected"},
		{UUID: unselectedID, Name: "unselected"},
	}

	selected := map[uuid.UUID]bool{selectedID: true}

	got := mapsToSync(maps, false, selected, nil)

	if len(got) != 1 || got[0].UUID != selectedID {
		t.Fatalf("mapsToSync() = %+v, want only the selected map", got)
	}
}

func TestMapsToSyncNewMapsAutoIncluded(t *testing.T) {
	t.Parallel()

	newID := uuid.New()
	knownID := uuid.New()

	maps := []store.MapRecord{
		{UUID: newID, Name: "new"},
		{UUID: knownID, Name: "known-but-unselected"},
	}

	known := map[uuid.UUID]bool{knownID: true}

	got := mapsToSync(maps, true, nil, known)

	if len(got) != 1 || got[0].UUID != newID {
		t.Fatalf("mapsToSync() = %+v, want only the new (not-yet-known) map", got)
	}
}

func TestMapsToSyncNewMapsDisabledExcludesUnselected(t *testing.T) {
	t.Parallel()

	maps := []store.MapRecord{
		{UUID: uuid.New(), Name: "unselected"},
	}

	got := mapsToSync(maps, false, nil, nil)

	if len(got) != 0 {
		t.Fatalf("mapsToSync() = %+v, want empty when nothing is selected and syncNewMaps is false", got)
	}
}
