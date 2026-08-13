package tilearchive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrUnsupportedFormat is returned by ExtractArchive when the input isn't a
// zip or tar(.gz) archive (as determined by sniffArchiveFormat, never by
// filename or Content-Type).
var ErrUnsupportedFormat = errors.New("unsupported archive format: must be zip or tar")

// ErrInvalidArchive wraps a failure while reading/extracting an
// otherwise-recognized zip or tar archive (corrupt data, etc).
var ErrInvalidArchive = errors.New("invalid archive file")

// A map version's extracted contents may only consist of numerically named
// directories (e.g. a z/x/y tile pyramid, validated via NumericSegmentRE)
// and numerically named .png files.
var numericPNGRE = regexp.MustCompile(`^[0-9]+\.png$`)

// ExtractArchive sniffs tmpPath's archive format and extracts it into a
// fresh staging directory created under dir, returning that directory's
// path. On success the caller is responsible for removing it; on failure
// ExtractArchive has already cleaned up any partial staging directory.
//
// The staging directory is created inside dir (not os.TempDir) so that a
// caller renaming it into its final place under dir is guaranteed to be a
// same-filesystem, and therefore atomic, rename.
func ExtractArchive(tmpPath, dir string) (stagingDir string, err error) {
	format, err := sniffArchiveFormat(tmpPath)
	if err != nil {
		return "", fmt.Errorf("inspect archive: %w", err)
	}

	if format == archiveUnknown {
		return "", ErrUnsupportedFormat
	}

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("prepare storage: %w", err)
	}

	stagingDir, err = os.MkdirTemp(dir, ".upload-*")
	if err != nil {
		return "", fmt.Errorf("prepare extraction: %w", err)
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
		return "", fmt.Errorf("%w: %v", ErrInvalidArchive, err) //nolint:errorlint // ErrInvalidArchive (the sentinel callers match via errors.Is) is the %w here; err's message is included via %v for context only
	}

	return stagingDir, nil
}

// archiveFormat identifies which extractor an archive needs.
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
// extractor to use, rather than trusting a client-supplied filename or
// Content-Type. zip and gzip both have unambiguous magic numbers at the
// start of the file; a plain (uncompressed) tar has no magic at offset 0,
// but the ustar format's magic string at byte offset 257 is a
// reliable-enough signal in practice.
func sniffArchiveFormat(path string) (archiveFormat, error) {
	f, err := os.Open(path) //nolint:gosec // path is a server-generated temp file name, not user input
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
	f, err := os.Open(tarPath) //nolint:gosec // tarPath is a server-generated temp file name, not user input
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
		log.Printf("tilearchive: skipping symlink entry %q", hdr.Name)
		return nil
	}

	isDir := hdr.Typeflag == tar.TypeDir

	name := hdr.Name
	if strings.Contains(name, "..") {
		log.Printf("tilearchive: skipping tar entry with parent traversal %q", name)
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
		log.Printf("tilearchive: skipping non-regular entry %q", hdr.Name)
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
			log.Printf("tilearchive: skipping symlink entry %q", f.Name)
			continue
		}

		if strings.Contains(f.Name, "..") {
			log.Printf("tilearchive: skipping zip entry with parent traversal %q", f.Name)
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

// resolveExtractTarget validates a zip/tar entry's name (using "/"
// separators, with a trailing "/" for directories) via
// validateExtractedEntryName, then resolves it to a target path within
// cleanDest. ok is false if the entry should be skipped — an invalid name,
// or a path that would escape cleanDest (zip-slip) — matching
// extractZip/extractTar's existing behavior of skipping bad entries (logged
// via log.Printf) rather than failing the whole upload.
func resolveExtractTarget(cleanDest, name string) (targetPath string, ok bool) {
	if err := validateExtractedEntryName(name); err != nil {
		log.Printf("tilearchive: skipping entry: %v", err)
		return "", false
	}

	targetPath = filepath.Join(cleanDest, strings.Trim(name, "/"))
	if targetPath != cleanDest && !strings.HasPrefix(targetPath, cleanDest+string(os.PathSeparator)) {
		log.Printf("tilearchive: skipping entry with illegal path %q", name)
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
		if !NumericSegmentRE.MatchString(seg) {
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
