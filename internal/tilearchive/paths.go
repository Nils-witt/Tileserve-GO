// Package tilearchive implements zip/tar archive extraction (with zip-slip
// protection) and tile-index maintenance for map version tile pyramids. It's
// shared by the HTTP upload handler (internal/handler) and the
// server-to-server sync puller (internal/sync), neither of which should
// depend on the other.
package tilearchive

import (
	"path/filepath"
	"regexp"

	"github.com/google/uuid"
)

// NumericSegmentRE matches a purely numeric path segment: a real map version
// identifier (see store.IncrementMapVersion, which always assigns
// stringified positive integers) or a z/x/y tile-pyramid directory name.
var NumericSegmentRE = regexp.MustCompile(`^[0-9]+$`)

// MapDir returns a map's storage directory under dataRoot.
func MapDir(dataRoot string, id uuid.UUID) string {
	return filepath.Join(dataRoot, id.String())
}

// MapVersionDir returns a specific version's extracted-tiles directory under dataRoot.
func MapVersionDir(dataRoot string, id uuid.UUID, version string) string {
	return filepath.Join(MapDir(dataRoot, id), version)
}
