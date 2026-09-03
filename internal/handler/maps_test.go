package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nilswitt.dev/tileserve-go/internal/handler/utils"
)

func TestWriteJSON(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	utils.WriteJSON(w, http.StatusCreated, map[string]string{"hello": "world"})

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}

	if got["hello"] != "world" {
		t.Errorf("body = %v, want {hello: world}", got)
	}
}

func TestDecodeJSON(t *testing.T) {
	t.Parallel()

	t.Run("valid body", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(`{"name":"a"}`))
		w := httptest.NewRecorder()

		var v struct {
			Name string `json:"name"`
		}
		if ok := utils.DecodeJSON(w, r, &v); !ok {
			t.Fatal("utils.DecodeJSON() = false, want true")
		}

		if v.Name != "a" {
			t.Errorf("decoded Name = %q, want %q", v.Name, "a")
		}

		if w.Code != http.StatusOK {
			t.Errorf("no response should have been written, but status = %d", w.Code)
		}
	})

	t.Run("invalid body", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(`not json`))
		w := httptest.NewRecorder()

		var v struct{}
		if ok := utils.DecodeJSON(w, r, &v); ok {
			t.Fatal("utils.DecodeJSON() = true, want false")
		}

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})
}

func TestRequireMethod(t *testing.T) {
	t.Parallel()

	t.Run("matching method", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)

		w := httptest.NewRecorder()
		if ok := utils.RequireMethod(w, r, http.MethodGet); !ok {
			t.Fatal("requireMethod() = false, want true")
		}

		if w.Code != http.StatusOK {
			t.Errorf("no response should have been written, but status = %d", w.Code)
		}
	})

	t.Run("mismatched method", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)

		w := httptest.NewRecorder()
		if ok := utils.RequireMethod(w, r, http.MethodGet); ok {
			t.Fatal("requireMethod() = true, want false")
		}

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
		}
	})
}

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
