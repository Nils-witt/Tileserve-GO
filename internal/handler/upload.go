package handler

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"nilswitt.dev/tileserve-go/internal/store"
)

// tileIndexBackfillConcurrency bounds how many overlay directories
// EnsureTileIndexes walks at once, so a data root with many maps doesn't
// open unbounded numbers of file descriptors concurrently.
const tileIndexBackfillConcurrency = 8

// maxUploadSize caps the size of an uploaded map version archive.
const maxUploadSize = 1 << 30 // 1 GiB

// A map version's extracted contents may only consist of numerically named
// directories (e.g. a z/x/y tile pyramid, validated via numericSegmentRE in
// maps.go) and numerically named .png files.
var numericPNGRE = regexp.MustCompile(`^[0-9]+\.png$`)

// uploadMapVersionHandler accepts a zip or tar (optionally gzip-compressed)
// archive as the raw request body, extracts it, and atomically bumps the
// map's current_version. Extraction happens into a staging directory next to
// the map's final location first; the DB version is only reserved (and the
// directory put in place) once extraction fully succeeds, so a bad upload
// never leaves the map pointing at a broken version.
func uploadMapVersionHandler(st *store.Store, dataRoot string, id uuid.UUID) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}

		if !requireMapPermission(w, r, st, id,
			func(p store.Permissions) bool { return p.CanCreate },
			func(mp store.MapPermission) bool { return mp.CanEdit },
		) {
			return
		}

		tmpPath, ok := receiveUpload(w, r)
		if !ok {
			return
		}
		defer func() { _ = os.Remove(tmpPath) }()

		dir := mapDir(dataRoot, id)

		stagingDir, ok := extractUploadedArchive(w, tmpPath, dir)
		if !ok {
			return
		}
		defer func() { _ = os.RemoveAll(stagingDir) }()

		if err := writeTileIndex(stagingDir); err != nil {
			http.Error(w, "failed to build tile index", http.StatusInternalServerError)
			return
		}

		m, err := st.IncrementMapVersion(r.Context(), id, usernameFromContext(r.Context()))
		if err != nil {
			writeStoreError(w, err, store.ErrMapNotFound, http.StatusNotFound, "map not found", "failed to record new version")
			return
		}

		destDir := filepath.Join(dir, m.CurrentVersion)
		if err := os.Rename(stagingDir, destDir); err != nil {
			http.Error(w, "failed to store version", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusCreated, m)
	}
}

// receiveUpload buffers r's body (capped at maxUploadSize) into a closed
// temp file and returns its path. On success the caller is responsible for
// removing it; on failure receiveUpload has already written the appropriate
// error response and cleaned up.
func receiveUpload(w http.ResponseWriter, r *http.Request) (string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	tmpFile, err := os.CreateTemp("", "tileserve-upload-*")
	if err != nil {
		http.Error(w, "failed to buffer upload", http.StatusInternalServerError)
		return "", false
	}
	defer func() { _ = tmpFile.Close() }()

	if _, err := io.Copy(tmpFile, r.Body); err != nil {
		_ = os.Remove(tmpFile.Name())

		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "upload too large", http.StatusRequestEntityTooLarge)
			return "", false
		}

		http.Error(w, "failed to read upload", http.StatusBadRequest)

		return "", false
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFile.Name())

		http.Error(w, "failed to buffer upload", http.StatusInternalServerError)

		return "", false
	}

	return tmpFile.Name(), true
}

// extractUploadedArchive sniffs tmpPath's archive format and extracts it
// into a fresh staging directory under dir, returning that directory's path.
// On success the caller is responsible for removing it; on failure
// extractUploadedArchive has already written the appropriate error response
// and cleaned up.
func extractUploadedArchive(w http.ResponseWriter, tmpPath, dir string) (string, bool) {
	format, err := sniffArchiveFormat(tmpPath)
	if err != nil {
		http.Error(w, "failed to inspect upload", http.StatusInternalServerError)
		return "", false
	}

	if format == archiveUnknown {
		http.Error(w, "unsupported archive format: must be zip or tar", http.StatusBadRequest)
		return "", false
	}

	if err := os.MkdirAll(dir, 0o750); err != nil {
		http.Error(w, "failed to prepare storage", http.StatusInternalServerError)
		return "", false
	}

	// Staged inside dir (not os.TempDir) so the final rename in
	// uploadMapVersionHandler is guaranteed to be same-filesystem and
	// therefore atomic.
	stagingDir, err := os.MkdirTemp(dir, ".upload-*")
	if err != nil {
		http.Error(w, "failed to prepare extraction", http.StatusInternalServerError)
		return "", false
	}

	switch format {
	case archiveZip:
		err = extractZip(tmpPath, stagingDir)
	case archiveTarGz:
		err = extractTar(tmpPath, stagingDir, true)
	case archiveTar:
		err = extractTar(tmpPath, stagingDir, false)
	case archiveUnknown:
		// Unreachable: the archiveUnknown check above already returned.
		err = errors.New("unsupported archive format")
	}

	if err != nil {
		_ = os.RemoveAll(stagingDir)

		http.Error(w, "invalid archive file: "+err.Error(), http.StatusBadRequest)

		return "", false
	}

	return stagingDir, true
}

// archiveFormat identifies which extractor an uploaded archive needs.
type archiveFormat int

const (
	archiveUnknown archiveFormat = iota
	archiveZip
	archiveTarGz
	archiveTar
)

var (
	zipMagic      = []byte("PK\x03\x04")
	zipEmptyMagic = []byte("PK\x05\x06")
	gzipMagic     = []byte{0x1f, 0x8b}
	tarMagic      = []byte("ustar")
)

// sniffArchiveFormat inspects path's leading bytes to determine which
// extractor to use, rather than trusting the client-supplied filename or
// Content-Type (the server never reads either). zip and gzip both have
// unambiguous magic numbers at the start of the file; a plain (uncompressed)
// tar has no magic at offset 0, but the ustar format's magic string at byte
// offset 257 is a reliable-enough signal in practice.
func sniffArchiveFormat(path string) (archiveFormat, error) {
	f, err := os.Open(path) //nolint:gosec // path is a server-generated os.CreateTemp name, not user input
	if err != nil {
		return archiveUnknown, fmt.Errorf("open upload: %w", err)
	}
	defer func() { _ = f.Close() }()

	header := make([]byte, 262)

	n, err := io.ReadFull(f, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return archiveUnknown, fmt.Errorf("read upload: %w", err)
	}

	header = header[:n]

	switch {
	case bytes.HasPrefix(header, zipMagic), bytes.HasPrefix(header, zipEmptyMagic):
		return archiveZip, nil
	case bytes.HasPrefix(header, gzipMagic):
		return archiveTarGz, nil
	case len(header) >= 262 && bytes.Equal(header[257:262], tarMagic):
		return archiveTar, nil
	default:
		return archiveUnknown, nil
	}
}

// extractTar extracts the tar archive at tarPath into destDir, transparently
// gunzipping first when gzipped is true. It applies the same zip-slip,
// symlink, and entry-name validation as extractZip, and likewise only
// returns an error for an unreadable archive or an actual filesystem
// failure while writing — invalid entries are skipped, not fatal.
func extractTar(tarPath, destDir string, gzipped bool) error {
	f, err := os.Open(tarPath) //nolint:gosec // tarPath is a server-generated os.CreateTemp name, not user input
	if err != nil {
		return fmt.Errorf("open tar: %w", err)
	}
	defer func() { _ = f.Close() }()

	var r io.Reader = f
	if gzipped {
		gr, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("open gzip: %w", err)
		}
		defer func() { _ = gr.Close() }()

		r = gr
	}

	cleanDest := filepath.Clean(destDir)

	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}

		if !strings.Contains(hdr.Name, "..") {
			if err := extractTarEntry(tr, hdr, cleanDest); err != nil {
				return err
			}
		}
	}

	return nil
}

// extractTarEntry extracts a single tar entry into cleanDest, applying the
// same zip-slip, symlink, and entry-name validation as extractZip's
// per-entry handling. It returns nil for a skipped entry (symlink,
// non-regular file, or invalid name) and only errors on an actual
// filesystem failure.
func extractTarEntry(tr *tar.Reader, hdr *tar.Header, cleanDest string) error {
	if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink {
		log.Printf("upload: skipping symlink entry %q", hdr.Name)
		return nil
	}

	isDir := hdr.Typeflag == tar.TypeDir

	name := hdr.Name
	if strings.Contains(name, "..") {
		log.Printf("upload: skipping tar entry with parent traversal %q", name)
		return nil
	}

	if isDir && !strings.HasSuffix(name, "/") {
		name += "/"
	}

	targetPath, ok := resolveExtractTarget(cleanDest, name)
	if !ok {
		return nil
	}

	if isDir {
		if err := os.MkdirAll(targetPath, 0o750); err != nil {
			return fmt.Errorf("create dir %s: %w", targetPath, err)
		}

		return nil
	}

	if hdr.Typeflag != tar.TypeReg {
		log.Printf("upload: skipping non-regular entry %q", hdr.Name)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o750); err != nil {
		return fmt.Errorf("create dir for %s: %w", targetPath, err)
	}

	return writeExtractedFile(tr, targetPath)
}

// extractZip extracts the zip file at zipPath into destDir. Entries that
// would escape destDir (zip-slip), that are symlinks, or whose name doesn't
// match the required numeric-directories/numeric-png-files layout are
// skipped rather than failing the whole upload. It only returns an error for
// an unreadable zip or an actual filesystem failure while writing.
func extractZip(zipPath, destDir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer func() { _ = zr.Close() }()

	cleanDest := filepath.Clean(destDir)

	for _, f := range zr.File {
		if f.Mode()&os.ModeSymlink != 0 {
			log.Printf("upload: skipping symlink entry %q", f.Name)
			continue
		}

		if strings.Contains(f.Name, "..") {
			log.Printf("upload: skipping zip entry with parent traversal %q", f.Name)
			continue
		}

		targetPath, ok := resolveExtractTarget(cleanDest, f.Name)
		if !ok {
			continue
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o750); err != nil {
				return fmt.Errorf("create dir %s: %w", targetPath, err)
			}

			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o750); err != nil {
			return fmt.Errorf("create dir for %s: %w", targetPath, err)
		}

		if err := extractZipFile(f, targetPath); err != nil {
			return err
		}
	}

	return nil
}

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

// writeTileIndex scans destDir's extracted z/x/y.png tile pyramid and writes
// an index.json listing every tile found (sorted by z, then x, then y).
func writeTileIndex(destDir string) error {
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
		// uploadMapVersionHandler), and that directory is renamed away
		// or removed once the upload finishes or fails.
		if !version.IsDir() || !numericSegmentRE.MatchString(version.Name()) {
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

		if err := writeTileIndex(versionDir); err != nil {
			return fmt.Errorf("write tile index for %s/%s: %w", overlayName, version.Name(), err)
		}
	}

	return nil
}

// resolveExtractTarget validates a zip/tar entry's name (using "/"
// separators, with a trailing "/" for directories) via
// validateExtractedEntryName, then resolves it to a target path within
// cleanDest. ok is false if the entry should be skipped — an invalid name,
// or a path that would escape cleanDest (zip-slip) — matching
// extractZip/extractTar's existing behavior of skipping bad entries (logged
// via log.Printf) rather than failing the whole upload.
func resolveExtractTarget(cleanDest, name string) (targetPath string, ok bool) {
	if err := validateExtractedEntryName(name); err != nil {
		log.Printf("upload: skipping entry: %v", err)
		return "", false
	}

	targetPath = filepath.Join(cleanDest, strings.Trim(name, "/"))
	if targetPath != cleanDest && !strings.HasPrefix(targetPath, cleanDest+string(os.PathSeparator)) {
		log.Printf("upload: skipping entry with illegal path %q", name)
		return "", false
	}

	return targetPath, true
}

// validateExtractedEntryName checks that a zip entry's path consists only of
// numeric directory segments, ending in either a numeric directory or a
// numeric ".png" file (e.g. "3/1/2.png"). name is the raw zip entry name,
// which uses "/" separators and a trailing "/" to mark directories.
func validateExtractedEntryName(name string) error {
	isDir := strings.HasSuffix(name, "/")

	segments := strings.Split(strings.Trim(name, "/"), "/")
	if len(segments) == 0 || segments[0] == "" {
		return fmt.Errorf("invalid entry name: %q", name)
	}

	dirSegments := segments
	if !isDir {
		dirSegments = segments[:len(segments)-1]
	}

	for _, seg := range dirSegments {
		if !numericSegmentRE.MatchString(seg) {
			return fmt.Errorf("invalid directory %q in %q: directory names must contain only digits", seg, name)
		}
	}

	if !isDir {
		last := segments[len(segments)-1]
		if !numericPNGRE.MatchString(last) {
			return fmt.Errorf("invalid file %q in %q: files must be named <number>.png", last, name)
		}
	}

	return nil
}

// extractZipFile copies a single zip entry's contents to targetPath,
// overwriting it if it already exists.
func extractZipFile(f *zip.File, targetPath string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open zip entry %s: %w", f.Name, err)
	}
	defer func() { _ = rc.Close() }()

	return writeExtractedFile(rc, targetPath)
}

// writeExtractedFile copies r's contents to targetPath, overwriting it if it
// already exists. Shared by the zip and tar extractors.
func writeExtractedFile(r io.Reader, targetPath string) error {
	//nolint:gosec // targetPath is confined to destDir by resolveExtractTarget's zip-slip/symlink checks
	out, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create file %s: %w", targetPath, err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, r); err != nil {
		return fmt.Errorf("write file %s: %w", targetPath, err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("close file %s: %w", targetPath, err)
	}

	return nil
}
