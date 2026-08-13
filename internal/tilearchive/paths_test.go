package tilearchive

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestMapDir(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	got := MapDir("/data/overlays", id)

	want := filepath.Join("/data/overlays", id.String())
	if got != want {
		t.Fatalf("MapDir() = %q, want %q", got, want)
	}
}

func TestMapVersionDir(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	got := MapVersionDir("/data/overlays", id, "3")

	want := filepath.Join("/data/overlays", id.String(), "3")
	if got != want {
		t.Fatalf("MapVersionDir() = %q, want %q", got, want)
	}

	if got != filepath.Join(MapDir("/data/overlays", id), "3") {
		t.Fatalf("MapVersionDir() should build on top of MapDir()")
	}
}
