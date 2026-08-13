package handler

import (
	"archive/zip"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"nilswitt.dev/tileserve-go/internal/tilearchive"
)

// mapVersionArchiveHandler streams a zip of an entire map version's
// extracted tile directory (tiles + index.json) in one response, so a
// server-to-server sync puller (internal/sync) can pull a whole version
// without one HTTP request per tile file. Unlike serveMapVersionFile, this
// is not reachable anonymously — it's gated the same way as bounds/
// geo-objects, via getViewableMap in the caller (routeMapVersionSubResource).
func mapVersionArchiveHandler(dataRoot string, id uuid.UUID, version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}

		// Defense in depth: ensure version is a single numeric segment before
		// using it in filesystem path construction.
		if !tilearchive.NumericSegmentRE.MatchString(version) {
			http.Error(w, "invalid version", http.StatusBadRequest)
			return
		}

		versionDir := tilearchive.MapVersionDir(dataRoot, id, version)

		//nolint:gosec // G703: versionDir is built from a validated version (see comment above), not raw request input
		if _, err := os.Stat(versionDir); err != nil {
			http.Error(w, "version not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="version.zip"`)

		zw := zip.NewWriter(w)
		defer func() { _ = zw.Close() }()

		// Errors from here on can't be surfaced as an HTTP status: headers
		// are already flushed once streaming starts. A failure mid-walk
		// just produces a truncated zip, which the puller's zip.OpenReader
		// on the fully-downloaded file naturally rejects — the standard
		// streaming-zip trade-off.
		//nolint:gosec // G703: versionDir is validated (see comment above versionDir's declaration), not raw request input
		_ = filepath.WalkDir(versionDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}

			rel, err := filepath.Rel(versionDir, path)
			if err != nil {
				return err
			}

			fw, err := zw.Create(filepath.ToSlash(rel))
			if err != nil {
				return err
			}

			f, err := os.Open(path) //nolint:gosec // path comes from WalkDir over versionDir, not user input
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()

			_, err = io.Copy(fw, f)

			return err
		})
	}
}
