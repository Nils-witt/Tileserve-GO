package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueryBoolParam(t *testing.T) {
	t.Parallel()

	t.Run("missing", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		w := httptest.NewRecorder()

		value, ok := queryBoolParam(w, r, "flag")
		if !ok || value != nil {
			t.Fatalf("queryBoolParam() = (%v, %v), want (nil, true)", value, ok)
		}

		if w.Code != http.StatusOK {
			t.Errorf("no response should have been written, but status = %d", w.Code)
		}
	})

	t.Run("valid", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?flag=true", nil)
		w := httptest.NewRecorder()

		value, ok := queryBoolParam(w, r, "flag")
		if !ok || value == nil || *value != true {
			t.Fatalf("queryBoolParam() = (%v, %v), want (true, true)", value, ok)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?flag=nope", nil)
		w := httptest.NewRecorder()

		value, ok := queryBoolParam(w, r, "flag")
		if ok || value != nil {
			t.Fatalf("queryBoolParam() = (%v, %v), want (nil, false)", value, ok)
		}

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})
}

func TestQueryFloatParam(t *testing.T) {
	t.Parallel()

	t.Run("missing", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		w := httptest.NewRecorder()

		value, ok := queryFloatParam(w, r, "lat")
		if !ok || value != nil {
			t.Fatalf("queryFloatParam() = (%v, %v), want (nil, true)", value, ok)
		}
	})

	t.Run("valid", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?lat=52.5", nil)
		w := httptest.NewRecorder()

		value, ok := queryFloatParam(w, r, "lat")
		if !ok || value == nil || *value != 52.5 {
			t.Fatalf("queryFloatParam() = (%v, %v), want (52.5, true)", value, ok)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?lat=nope", nil)
		w := httptest.NewRecorder()

		value, ok := queryFloatParam(w, r, "lat")
		if ok || value != nil {
			t.Fatalf("queryFloatParam() = (%v, %v), want (nil, false)", value, ok)
		}

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})
}

func TestGeoObjectFilterFromQuery(t *testing.T) {
	t.Parallel()

	t.Run("no bbox params", func(t *testing.T) {
		t.Parallel()
		testGeoObjectFilterNoBBoxParams(t)
	})

	t.Run("full bbox", func(t *testing.T) {
		t.Parallel()
		testGeoObjectFilterFullBBox(t)
	})

	t.Run("partial bbox is rejected", func(t *testing.T) {
		t.Parallel()
		testGeoObjectFilterPartialBBoxRejected(t)
	})

	t.Run("invalid bbox number", func(t *testing.T) {
		t.Parallel()
		testGeoObjectFilterInvalidBBoxNumber(t)
	})
}

func testGeoObjectFilterNoBBoxParams(t *testing.T) {
	t.Helper()

	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?name=hydrant&createdBy=bob", nil)
	w := httptest.NewRecorder()

	filter, ok := geoObjectFilterFromQuery(w, r)
	if !ok {
		t.Fatal("geoObjectFilterFromQuery() ok = false, want true")
	}

	if filter.Name != "hydrant" || filter.CreatedBy != "bob" {
		t.Errorf("filter = %+v, want Name=hydrant CreatedBy=bob", filter)
	}

	if filter.MinLat != nil {
		t.Errorf("filter.MinLat = %v, want nil", filter.MinLat)
	}
}

func testGeoObjectFilterFullBBox(t *testing.T) {
	t.Helper()

	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?minLat=1&maxLat=2&minLon=3&maxLon=4", nil)
	w := httptest.NewRecorder()

	filter, ok := geoObjectFilterFromQuery(w, r)
	if !ok {
		t.Fatal("geoObjectFilterFromQuery() ok = false, want true")
	}

	if filter.MinLat == nil || *filter.MinLat != 1 || filter.MaxLon == nil || *filter.MaxLon != 4 {
		t.Errorf("filter bbox = %+v, want minLat=1 maxLon=4", filter)
	}
}

func testGeoObjectFilterPartialBBoxRejected(t *testing.T) {
	t.Helper()

	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?minLat=1&maxLat=2", nil)
	w := httptest.NewRecorder()

	_, ok := geoObjectFilterFromQuery(w, r)
	if ok {
		t.Fatal("geoObjectFilterFromQuery() ok = true, want false for a partial bbox")
	}

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func testGeoObjectFilterInvalidBBoxNumber(t *testing.T) {
	t.Helper()

	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?minLat=nope", nil)
	w := httptest.NewRecorder()

	_, ok := geoObjectFilterFromQuery(w, r)
	if ok {
		t.Fatal("geoObjectFilterFromQuery() ok = true, want false for an invalid number")
	}

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
