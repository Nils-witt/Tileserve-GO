package tilearchive

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"
)

// tileIndexBackfillConcurrency bounds how many overlay directories
// EnsureTileIndexes walks at once, so a data root with many maps doesn't
// open unbounded numbers of file descriptors concurrently.
const tileIndexBackfillConcurrency = 8

// tileIndex lists every tile extracted into a map version, written as
// index.json alongside the tiles so a client can enumerate what's available
// without probing z/x/y coordinates blindly.
type tileIndex struct {
	Tiles []tileCoord `json:"tiles"`
}

type tileCoord struct {
	Z int `json:"z"`
	X int `json:"x"`
	Y int `json:"y"`
}

// WriteTileIndex scans destDir's extracted z/x/y.png tile pyramid and writes
// an index.json listing every tile found (sorted by z, then x, then y).
func WriteTileIndex(destDir string) error {
	tiles, err := scanTileCoords(destDir)
	if err != nil {
		return err
	}

	sort.Slice(tiles, func(i, j int) bool {
		if tiles[i].Z != tiles[j].Z {
			return tiles[i].Z < tiles[j].Z
		}

		if tiles[i].X != tiles[j].X {
			return tiles[i].X < tiles[j].X
		}

		return tiles[i].Y < tiles[j].Y
	})

	data, err := json.Marshal(tileIndex{Tiles: tiles})
	if err != nil {
		return fmt.Errorf("marshal tile index: %w", err)
	}

	if err := os.WriteFile(filepath.Join(destDir, "index.json"), data, 0o600); err != nil {
		return fmt.Errorf("write tile index: %w", err)
	}

	return nil
}

// scanTileCoords walks destDir's extracted z/x/y.png tile pyramid and
// returns every tile coordinate found, in arbitrary order.
func scanTileCoords(destDir string) ([]tileCoord, error) {
	zEntries, err := os.ReadDir(destDir)
	if err != nil {
		return nil, fmt.Errorf("read version dir: %w", err)
	}

	var tiles []tileCoord

	for _, ze := range zEntries {
		if !ze.IsDir() {
			continue
		}

		z, err := strconv.Atoi(ze.Name())
		if err != nil {
			continue
		}

		zTiles, err := scanZoomTiles(destDir, ze.Name(), z)
		if err != nil {
			return nil, err
		}

		tiles = append(tiles, zTiles...)
	}

	return tiles, nil
}

// scanZoomTiles walks a single zoom level directory (destDir/zName) for its
// x/y.png tiles.
func scanZoomTiles(destDir, zName string, z int) ([]tileCoord, error) {
	xEntries, err := os.ReadDir(filepath.Join(destDir, zName))
	if err != nil {
		return nil, fmt.Errorf("read zoom dir %s: %w", zName, err)
	}

	var tiles []tileCoord

	for _, xe := range xEntries {
		if !xe.IsDir() {
			continue
		}

		x, err := strconv.Atoi(xe.Name())
		if err != nil {
			continue
		}

		yEntries, err := os.ReadDir(filepath.Join(destDir, zName, xe.Name()))
		if err != nil {
			return nil, fmt.Errorf("read x dir %s/%s: %w", zName, xe.Name(), err)
		}

		for _, ye := range yEntries {
			y, err := strconv.Atoi(strings.TrimSuffix(ye.Name(), ".png"))
			if err != nil || !numericPNGRE.MatchString(ye.Name()) {
				continue
			}

			tiles = append(tiles, tileCoord{Z: z, X: x, Y: y})
		}
	}

	return tiles, nil
}

// EnsureTileIndexes walks dataRoot for every map ("overlay") directory and
// each of its numeric version subdirectories, generating a missing
// index.json for any version that doesn't already have one. It's meant to be
// run once at startup so versions extracted before index.json existed (or a
// version where a prior index write failed) get backfilled without
// requiring a re-upload. Overlays are processed concurrently (bounded by
// tileIndexBackfillConcurrency), since each overlay's backfill is
// independent I/O.
func EnsureTileIndexes(dataRoot string) error {
	overlays, err := os.ReadDir(dataRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("read data root: %w", err)
	}

	g := new(errgroup.Group)
	g.SetLimit(tileIndexBackfillConcurrency)

	for _, overlay := range overlays {
		if !overlay.IsDir() {
			continue
		}

		overlayName := overlay.Name()

		g.Go(func() error {
			return ensureOverlayTileIndexes(dataRoot, overlayName)
		})
	}

	return g.Wait()
}

// ensureOverlayTileIndexes backfills index.json for every numeric version
// subdirectory of a single overlay ("map") directory. See EnsureTileIndexes.
func ensureOverlayTileIndexes(dataRoot, overlayName string) error {
	overlayDir := filepath.Join(dataRoot, overlayName)

	versions, err := os.ReadDir(overlayDir)
	if err != nil {
		return fmt.Errorf("read overlay dir %s: %w", overlayName, err)
	}

	for _, version := range versions {
		// Non-numeric entries are skipped rather than treated as an
		// error: in-progress uploads stage extraction in a
		// ".upload-*" directory right next to finished versions (see
		// ExtractArchive), and that directory is renamed away or
		// removed once the upload finishes or fails.
		if !version.IsDir() || !NumericSegmentRE.MatchString(version.Name()) {
			continue
		}

		versionDir := filepath.Join(overlayDir, version.Name())

		_, err := os.Stat(filepath.Join(versionDir, "index.json"))
		if err == nil {
			continue
		}

		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat index.json for %s/%s: %w", overlayName, version.Name(), err)
		}

		log.Printf("backfilling missing tile index for overlay %s version %s", overlayName, version.Name())

		if err := WriteTileIndex(versionDir); err != nil {
			return fmt.Errorf("write tile index for %s/%s: %w", overlayName, version.Name(), err)
		}
	}

	return nil
}
