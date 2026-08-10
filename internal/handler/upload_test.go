package handler

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// testTileData is the placeholder file content used for fixture tile entries
// throughout this file.
const testTileData = "tile-data"

// buildZip writes a zip archive to path containing the given entries. A
// trailing "/" in name marks a directory entry.
func buildZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

// buildTar writes a tar archive (optionally gzip-compressed) to path
// containing the given regular-file entries.
func buildTar(t *testing.T, path string, gzipped bool, entries map[string]string) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = f.Close() }()

	var (
		w  = io.Writer(f)
		gw *gzip.Writer
	)
	if gzipped {
		gw = gzip.NewWriter(f)
		w = gw
	}

	tw := tar.NewWriter(w)

	for name, content := range entries {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}

		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if gw != nil {
		if err := gw.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResolveExtractTarget(t *testing.T) {
	t.Parallel()

	dest := t.TempDir()
	cleanDest := filepath.Clean(dest)

	tests := []struct {
		name       string
		entry      string
		wantOK     bool
		wantTarget string
	}{
		{name: "valid directory entry", entry: "3/1/", wantOK: true, wantTarget: filepath.Join(cleanDest, "3", "1")},
		{name: "valid file entry", entry: "3/1/2.png", wantOK: true, wantTarget: filepath.Join(cleanDest, "3", "1", "2.png")},
		{name: "zip-slip attempt", entry: "../../etc/passwd", wantOK: false},
		{name: "non-numeric directory segment", entry: "3/notes.txt", wantOK: false},
		{name: "file not matching <number>.png", entry: "3/1/2.jpg", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			target, ok := resolveExtractTarget(cleanDest, tc.entry)
			if ok != tc.wantOK {
				t.Fatalf("resolveExtractTarget(%q) ok = %v, want %v", tc.entry, ok, tc.wantOK)
			}

			if ok && target != tc.wantTarget {
				t.Fatalf("resolveExtractTarget(%q) target = %q, want %q", tc.entry, target, tc.wantTarget)
			}
		})
	}
}

func TestSniffArchiveFormat(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	zipPath := filepath.Join(dir, "a.zip")
	buildZip(t, zipPath, map[string]string{"1/2/3.png": "tile"})

	if got, err := sniffArchiveFormat(zipPath); err != nil || got != archiveZip {
		t.Fatalf("zip: got %v, %v; want archiveZip, nil", got, err)
	}

	tarGzPath := filepath.Join(dir, "a.tar.gz")
	buildTar(t, tarGzPath, true, map[string]string{"1/2/3.png": "tile"})

	if got, err := sniffArchiveFormat(tarGzPath); err != nil || got != archiveTarGz {
		t.Fatalf("tar.gz: got %v, %v; want archiveTarGz, nil", got, err)
	}

	tarPath := filepath.Join(dir, "a.tar")
	buildTar(t, tarPath, false, map[string]string{"1/2/3.png": "tile"})

	if got, err := sniffArchiveFormat(tarPath); err != nil || got != archiveTar {
		t.Fatalf("tar: got %v, %v; want archiveTar, nil", got, err)
	}

	unknownPath := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(unknownPath, []byte("not an archive"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got, err := sniffArchiveFormat(unknownPath); err != nil || got != archiveUnknown {
		t.Fatalf("unknown: got %v, %v; want archiveUnknown, nil", got, err)
	}
}

func TestExtractZip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	archivePath := filepath.Join(dir, "up.zip")

	destDir := filepath.Join(dir, "dest")
	if err := os.Mkdir(destDir, 0o750); err != nil {
		t.Fatal(err)
	}

	buildZip(t, archivePath, map[string]string{
		"3/1/2.png":        testTileData,
		"../../etc/passwd": "escape attempt",
		"3/notes.txt":      "invalid entry name",
	})

	if err := extractZip(archivePath, destDir); err != nil {
		t.Fatalf("extractZip: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(destDir, "3", "1", "2.png"))
	if err != nil {
		t.Fatalf("expected tile written: %v", err)
	}

	if string(got) != testTileData {
		t.Fatalf("tile content = %q, want %q", got, testTileData)
	}

	if _, err := os.Stat(filepath.Join(dir, "etc", "passwd")); !os.IsNotExist(err) {
		t.Fatalf("zip-slip entry should not have escaped destDir")
	}

	if _, err := os.Stat(filepath.Join(destDir, "3", "notes.txt")); !os.IsNotExist(err) {
		t.Fatalf("invalidly named entry should have been skipped")
	}
}

func TestExtractTar(t *testing.T) {
	t.Parallel()

	for _, gzipped := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "gzip"}[gzipped], func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			archivePath := filepath.Join(dir, "up.tar")

			destDir := filepath.Join(dir, "dest")
			if err := os.Mkdir(destDir, 0o750); err != nil {
				t.Fatal(err)
			}

			buildTar(t, archivePath, gzipped, map[string]string{
				"5/10/20.png":      testTileData,
				"../../etc/passwd": "escape attempt",
				"5/notes.txt":      "invalid entry name",
			})

			if err := extractTar(archivePath, destDir, gzipped); err != nil {
				t.Fatalf("extractTar: %v", err)
			}

			got, err := os.ReadFile(filepath.Join(destDir, "5", "10", "20.png"))
			if err != nil {
				t.Fatalf("expected tile written: %v", err)
			}

			if string(got) != testTileData {
				t.Fatalf("tile content = %q, want %q", got, testTileData)
			}

			if _, err := os.Stat(filepath.Join(dir, "etc", "passwd")); !os.IsNotExist(err) {
				t.Fatalf("tar-slip entry should not have escaped destDir")
			}

			if _, err := os.Stat(filepath.Join(destDir, "5", "notes.txt")); !os.IsNotExist(err) {
				t.Fatalf("invalidly named entry should have been skipped")
			}
		})
	}
}

func TestWriteTileIndex(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	archivePath := filepath.Join(dir, "up.zip")

	destDir := filepath.Join(dir, "dest")
	if err := os.Mkdir(destDir, 0o750); err != nil {
		t.Fatal(err)
	}

	buildZip(t, archivePath, map[string]string{
		"3/1/2.png": testTileData,
		"3/1/5.png": testTileData,
		"1/0/0.png": testTileData,
	})

	if err := extractZip(archivePath, destDir); err != nil {
		t.Fatalf("extractZip: %v", err)
	}

	if err := writeTileIndex(destDir); err != nil {
		t.Fatalf("writeTileIndex: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(destDir, "index.json"))
	if err != nil {
		t.Fatalf("expected index.json written: %v", err)
	}

	var idx tileIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("index.json is not valid JSON: %v", err)
	}

	want := []tileCoord{
		{Z: 1, X: 0, Y: 0},
		{Z: 3, X: 1, Y: 2},
		{Z: 3, X: 1, Y: 5},
	}
	if len(idx.Tiles) != len(want) {
		t.Fatalf("tiles = %+v, want %+v", idx.Tiles, want)
	}

	for i, tile := range idx.Tiles {
		if tile != want[i] {
			t.Fatalf("tiles[%d] = %+v, want %+v", i, tile, want[i])
		}
	}
}

// mustMkdirAll is os.MkdirAll, failing the test on error.
func mustMkdirAll(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatal(err)
	}
}

// mustWriteFile is os.WriteFile, failing the test on error.
func mustWriteFile(t *testing.T, path string, content []byte) {
	t.Helper()

	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureTileIndexes(t *testing.T) {
	t.Parallel()

	dataRoot := t.TempDir()

	// overlay "a": version "1" already has an (arbitrary, pre-existing)
	// index.json that must be left untouched.
	v1Dir := filepath.Join(dataRoot, "a", "1")
	mustMkdirAll(t, v1Dir)
	mustWriteFile(t, filepath.Join(v1Dir, "index.json"), []byte("existing"))

	// overlay "a": version "2" is missing index.json and has tiles.
	v2Dir := filepath.Join(dataRoot, "a", "2")
	mustMkdirAll(t, filepath.Join(v2Dir, "4", "8"))
	mustWriteFile(t, filepath.Join(v2Dir, "4", "8", "16.png"), []byte("tile"))

	// overlay "a": an in-progress upload staging dir, which must be skipped
	// (non-numeric name).
	mustMkdirAll(t, filepath.Join(dataRoot, "a", ".upload-123"))

	// overlay "b": version "1" missing index.json, no tiles at all.
	v3Dir := filepath.Join(dataRoot, "b", "1")
	mustMkdirAll(t, v3Dir)

	if err := EnsureTileIndexes(dataRoot); err != nil {
		t.Fatalf("EnsureTileIndexes: %v", err)
	}

	assertTileIndexesBackfilled(t, dataRoot, v1Dir, v2Dir, v3Dir)
}

// assertTileIndexesBackfilled checks the postconditions of the
// TestEnsureTileIndexes scenario above: a's pre-existing index.json is
// untouched, a/2 and b/1 got backfilled with the right tile lists, and the
// in-progress upload staging dir was left alone.
func assertTileIndexesBackfilled(t *testing.T, dataRoot, v1Dir, v2Dir, v3Dir string) {
	t.Helper()

	got, err := os.ReadFile(filepath.Join(v1Dir, "index.json"))
	if err != nil || string(got) != "existing" {
		t.Fatalf("pre-existing index.json was overwritten: got %q, %v", got, err)
	}

	data, err := os.ReadFile(filepath.Join(v2Dir, "index.json"))
	if err != nil {
		t.Fatalf("expected index.json backfilled for a/2: %v", err)
	}

	var idx tileIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("a/2 index.json is not valid JSON: %v", err)
	}

	if want := []tileCoord{{Z: 4, X: 8, Y: 16}}; len(idx.Tiles) != 1 || idx.Tiles[0] != want[0] {
		t.Fatalf("a/2 tiles = %+v, want %+v", idx.Tiles, want)
	}

	data, err = os.ReadFile(filepath.Join(v3Dir, "index.json"))
	if err != nil {
		t.Fatalf("expected index.json backfilled for b/1: %v", err)
	}

	if err := json.Unmarshal(data, &idx); err != nil || len(idx.Tiles) != 0 {
		t.Fatalf("b/1 index.json = %q, want empty tile list", data)
	}

	if _, err := os.Stat(filepath.Join(dataRoot, "a", ".upload-123", "index.json")); !os.IsNotExist(err) {
		t.Fatalf("staging dir should not have gotten an index.json")
	}
}

func TestExtractZipAndTarProduceEquivalentOutput(t *testing.T) {
	t.Parallel()

	entries := map[string]string{
		"7/3/4.png": "same-tile",
	}

	dir := t.TempDir()
	zipPath := filepath.Join(dir, "up.zip")
	tarPath := filepath.Join(dir, "up.tar")
	zipDest := filepath.Join(dir, "zip-dest")
	tarDest := filepath.Join(dir, "tar-dest")

	if err := os.MkdirAll(zipDest, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(tarDest, 0o750); err != nil {
		t.Fatal(err)
	}

	buildZip(t, zipPath, entries)
	buildTar(t, tarPath, false, entries)

	if err := extractZip(zipPath, zipDest); err != nil {
		t.Fatalf("extractZip: %v", err)
	}

	if err := extractTar(tarPath, tarDest, false); err != nil {
		t.Fatalf("extractTar: %v", err)
	}

	zipContent, err := os.ReadFile(filepath.Join(zipDest, "7", "3", "4.png"))
	if err != nil {
		t.Fatal(err)
	}

	tarContent, err := os.ReadFile(filepath.Join(tarDest, "7", "3", "4.png"))
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(zipContent, tarContent) {
		t.Fatalf("zip and tar extraction diverged: %q vs %q", zipContent, tarContent)
	}
}
