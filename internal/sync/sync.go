package sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"nilswitt.dev/tileserve-go/internal/store"
	"nilswitt.dev/tileserve-go/internal/tilearchive"
)

// maxConcurrentMapSyncs bounds how many of a remote's maps are synced at
// once, mirroring tilearchive.EnsureTileIndexes's concurrency-bounding
// rationale — but smaller, since each unit of work here can be a large
// archive download rather than just a filesystem walk.
const maxConcurrentMapSyncs = 4

// versionsToPull returns the versions in remote not yet present locally
// (per alreadyLocal), oldest first, so a sync interrupted partway through
// resumes cleanly rather than needing to restart from the newest version.
// Pure and DB-free so it's directly unit-testable.
func versionsToPull(alreadyLocal map[string]bool, remote []store.MapVersionRecord) []store.MapVersionRecord {
	var toPull []store.MapVersionRecord

	for _, v := range remote {
		if !alreadyLocal[v.Version] {
			toPull = append(toPull, v)
		}
	}

	sort.Slice(toPull, func(i, j int) bool {
		return toPull[i].CreatedAt.Before(toPull[j].CreatedAt)
	})

	return toPull
}

// reconcileAliases compares local against remote alias state and reports
// what's needed to make local match: aliases to create or repoint (toSet,
// covering both new aliases and ones whose target version changed) and
// alias names to delete (present locally, absent remotely). Pure and
// DB-free so it's directly unit-testable.
func reconcileAliases(local, remote []store.MapVersionAlias) (toSet []store.MapVersionAlias, toDelete []string) {
	localByAlias := make(map[string]store.MapVersionAlias, len(local))
	for _, a := range local {
		localByAlias[a.Alias] = a
	}

	remoteByAlias := make(map[string]store.MapVersionAlias, len(remote))
	for _, a := range remote {
		remoteByAlias[a.Alias] = a
	}

	for _, r := range remote {
		if l, ok := localByAlias[r.Alias]; !ok || l.Version != r.Version {
			toSet = append(toSet, r)
		}
	}

	for _, l := range local {
		if _, ok := remoteByAlias[l.Alias]; !ok {
			toDelete = append(toDelete, l.Alias)
		}
	}

	return toSet, toDelete
}

// syncRemoteOnce performs one full-mirror sync pass against remote: every
// map visible to its API key is mirrored locally (see store.UpsertSyncedMap),
// including every version and alias not yet present locally. Maps are
// synced concurrently, bounded by maxConcurrentMapSyncs.
func syncRemoteOnce(ctx context.Context, st *store.Store, dataRoot string, client *Client, remote store.SyncRemote) error {
	maps, err := client.ListMaps(ctx)
	if err != nil {
		return fmt.Errorf("list remote maps: %w", err)
	}

	actor := "sync:" + remote.Name

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrentMapSyncs)

	for _, rm := range maps {
		g.Go(func() error {
			if err := syncMap(gctx, st, dataRoot, client, remote, rm, actor); err != nil {
				return fmt.Errorf("sync map %s: %w", rm.UUID, err)
			}

			return nil
		})
	}

	return g.Wait()
}

// syncMap mirrors one remote map locally: ensures the local map row exists
// under the same UUID, pulls every version not yet present, reconciles
// aliases, then advances current_version once its target is confirmed
// present. Aliases and current_version are handled only after every version
// has been pulled, since aliases FK-reference map_versions — if the
// remote's current_version or an alias races ahead of what's been pulled,
// that's picked up on a later tick rather than treated as an error now.
func syncMap(ctx context.Context, st *store.Store, dataRoot string, client *Client, remote store.SyncRemote, rm store.MapRecord, actor string) error {
	if _, err := st.UpsertSyncedMap(ctx, rm.UUID, rm.Name, rm.VisibleToAll, rm.AnonymousAllowed, remote.ID, actor); err != nil {
		return fmt.Errorf("upsert local map: %w", err)
	}

	remoteVersions, err := client.ListVersions(ctx, rm.UUID)
	if err != nil {
		return fmt.Errorf("list remote versions: %w", err)
	}

	alreadyLocal, err := localVersionSet(ctx, st, rm.UUID, remoteVersions)
	if err != nil {
		return err
	}

	for _, v := range versionsToPull(alreadyLocal, remoteVersions) {
		if err := pullVersion(ctx, st, dataRoot, client, rm.UUID, v); err != nil {
			return fmt.Errorf("pull version %s: %w", v.Version, err)
		}
	}

	if err := reconcileMapAliases(ctx, st, client, rm.UUID, actor); err != nil {
		return fmt.Errorf("reconcile aliases: %w", err)
	}

	if rm.CurrentVersion == "" {
		return nil
	}

	if err := st.SetSyncedCurrentVersion(ctx, rm.UUID, rm.CurrentVersion); err != nil && !errors.Is(err, store.ErrSyncedVersionNotRecorded) {
		return fmt.Errorf("set current version: %w", err)
	}

	return nil
}

// localVersionSet reports, for each of remoteVersions, whether it's already
// recorded locally.
func localVersionSet(ctx context.Context, st *store.Store, mapID uuid.UUID, remoteVersions []store.MapVersionRecord) (map[string]bool, error) {
	alreadyLocal := make(map[string]bool, len(remoteVersions))

	for _, v := range remoteVersions {
		has, err := st.HasMapVersion(ctx, mapID, v.Version)
		if err != nil {
			return nil, fmt.Errorf("check local version %s: %w", v.Version, err)
		}

		alreadyLocal[v.Version] = has
	}

	return alreadyLocal, nil
}

// reconcileMapAliases fetches mapID's alias state from both sides and
// applies reconcileAliases's decision via the store. An alias whose target
// version hasn't been pulled yet (the remote raced ahead) is skipped for
// now rather than treated as an error — it's picked up on a later tick.
func reconcileMapAliases(ctx context.Context, st *store.Store, client *Client, mapID uuid.UUID, actor string) error {
	remoteAliases, err := client.ListAliases(ctx, mapID)
	if err != nil {
		return fmt.Errorf("list remote aliases: %w", err)
	}

	localAliases, err := st.ListMapVersionAliases(ctx, mapID)
	if err != nil {
		return fmt.Errorf("list local aliases: %w", err)
	}

	toSet, toDelete := reconcileAliases(localAliases, remoteAliases)

	for _, a := range toSet {
		has, err := st.HasMapVersion(ctx, mapID, a.Version)
		if err != nil {
			return fmt.Errorf("check alias target version %s: %w", a.Version, err)
		}

		if !has {
			continue
		}

		if _, err := st.SetMapVersionAlias(ctx, mapID, a.Alias, a.Version, actor); err != nil {
			return fmt.Errorf("set alias %s: %w", a.Alias, err)
		}
	}

	for _, alias := range toDelete {
		if err := st.DeleteMapVersionAlias(ctx, mapID, alias); err != nil {
			return fmt.Errorf("delete alias %s: %w", alias, err)
		}
	}

	return nil
}

// pullVersion downloads and extracts one map version from the remote,
// following store.IncrementMapVersion's own crash-safety philosophy:
// map_versions is the single source of truth for "is this version done."
// Order of operations:
//
//  1. Skip entirely if already recorded (no network needed).
//  2. Download the archive to a temp file.
//  3. Extract it into a staging directory.
//  4. Rebuild the tile index (defensive: the remote's own index.json is
//     already included in the archive, but this guarantees local
//     consistency even if that file were ever missing or stale).
//  5. Defensively remove any orphaned complete-but-unrecorded directory
//     left by a prior crash between steps 6 and 7.
//  6. Atomically rename the staging directory into place.
//  7. Record the version — only after this does HasMapVersion see it as
//     done.
//
// A crash between steps 6 and 7 just means the next sync tick redoes the
// download and extraction (safe, because of step 5's cleanup) — wasteful,
// never incorrect.
func pullVersion(ctx context.Context, st *store.Store, dataRoot string, client *Client, mapID uuid.UUID, v store.MapVersionRecord) error {
	has, err := st.HasMapVersion(ctx, mapID, v.Version)
	if err != nil {
		return fmt.Errorf("check existing version: %w", err)
	}

	if has {
		return nil
	}

	tmpPath, err := client.DownloadArchive(ctx, mapID, v.Version)
	if err != nil {
		return fmt.Errorf("download archive: %w", err)
	}
	defer func() { _ = os.Remove(tmpPath) }()

	dir := tilearchive.MapDir(dataRoot, mapID)

	stagingDir, err := tilearchive.ExtractArchive(tmpPath, dir)
	if err != nil {
		return fmt.Errorf("extract archive: %w", err)
	}
	defer func() { _ = os.RemoveAll(stagingDir) }()

	if err := tilearchive.WriteTileIndex(stagingDir); err != nil {
		return fmt.Errorf("write tile index: %w", err)
	}

	destDir := filepath.Join(dir, v.Version)

	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("clear orphaned version dir: %w", err)
	}

	if err := os.Rename(stagingDir, destDir); err != nil {
		return fmt.Errorf("store version: %w", err)
	}

	if err := st.RecordSyncedMapVersion(ctx, mapID, v.Version, v.CreatedBy, v.CreatedAt); err != nil {
		return fmt.Errorf("record version: %w", err)
	}

	return nil
}
