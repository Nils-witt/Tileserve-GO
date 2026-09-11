package httputil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	WriteJSON(w, http.StatusCreated, map[string]string{"hello": "world"})

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
		if ok := DecodeJSON(w, r, &v); !ok {
			t.Fatal("DecodeJSON() = false, want true")
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
		if ok := DecodeJSON(w, r, &v); ok {
			t.Fatal("DecodeJSON() = true, want false")
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
		if ok := RequireMethod(w, r, http.MethodGet); !ok {
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
		if ok := RequireMethod(w, r, http.MethodGet); ok {
			t.Fatal("requireMethod() = true, want false")
		}

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
		}
	})
}
