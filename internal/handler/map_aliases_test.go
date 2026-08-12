package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateAliasName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		alias string
		want  bool
	}{
		{"empty", "", false},
		{"reserved current keyword", "current", false},
		{"numeric", "3", false},
		{"numeric with leading zero", "007", false},
		{"zero", "0", false},
		{"alphabetic alias", "stable", true},
		{"mixed alnum alias", "v2", true},
		{"alias containing current as substring", "current2", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			got := validateAliasName(w, tt.alias)

			if got != tt.want {
				t.Errorf("validateAliasName(%q) = %v, want %v", tt.alias, got, tt.want)
			}

			if tt.want && w.Code != http.StatusOK {
				t.Errorf("no response should have been written for a valid alias, but status = %d", w.Code)
			}

			if !tt.want && w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestClassifyVersionSegment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		segment string
		want    versionSegmentKind
	}{
		{"current keyword", "current", versionSegmentCurrent},
		{"numeric", "3", versionSegmentNumeric},
		{"numeric zero", "0", versionSegmentNumeric},
		{"alias-shaped", "stable", versionSegmentAlias},
		{"alias-shaped mixed alnum", "v2", versionSegmentAlias},
		{"alias containing current as substring", "current2", versionSegmentAlias},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := classifyVersionSegment(tt.segment); got != tt.want {
				t.Errorf("classifyVersionSegment(%q) = %v, want %v", tt.segment, got, tt.want)
			}
		})
	}
}
