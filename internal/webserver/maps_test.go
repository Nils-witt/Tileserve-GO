package webserver

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteStoreError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("sentinel error")

	t.Run("sentinel error maps to its own status and message", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		writeStoreError(w, sentinel, sentinel, http.StatusNotFound, "not found", "internal failure")

		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
		}

		if body := strings.TrimSpace(w.Body.String()); body != "not found" {
			t.Errorf("body = %q, want %q", body, "not found")
		}
	})

	t.Run("wrapped sentinel error still matches via errors.Is", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		wrapped := fmt.Errorf("context: %w", sentinel)
		writeStoreError(w, wrapped, sentinel, http.StatusBadRequest, "bad request", "internal failure")

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("other error falls back to 500", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		writeStoreError(w, errors.New("boom"), sentinel, http.StatusNotFound, "not found", "internal failure")

		if w.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
		}

		if body := strings.TrimSpace(w.Body.String()); body != "internal failure" {
			t.Errorf("body = %q, want %q", body, "internal failure")
		}
	})
}
