package sync

import (
	"testing"
	"time"

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
