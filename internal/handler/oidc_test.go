package handler

import "testing"

const uiDefaultPath = "/ui/"

func TestSanitizeRedirectPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "empty defaults to ui", path: "", want: uiDefaultPath},
		{name: "valid absolute path", path: "/login", want: "/login"},
		{name: "valid nested path", path: "/ui/", want: uiDefaultPath},
		{name: "relative path rejected", path: "login", want: uiDefaultPath},
		{name: "protocol-relative rejected", path: "//evil.example.com", want: uiDefaultPath},
		{name: "absolute url rejected", path: "https://evil.example.com/", want: uiDefaultPath},
		{name: "backslash rejected", path: "/\\evil.example.com", want: uiDefaultPath},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := sanitizeRedirectPath(tc.path); got != tc.want {
				t.Errorf("sanitizeRedirectPath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestFirstNonEmpty(t *testing.T) {
	t.Parallel()

	if got := firstNonEmpty("", "", "c"); got != "c" {
		t.Errorf("firstNonEmpty() = %q, want %q", got, "c")
	}

	if got := firstNonEmpty("a", "b"); got != "a" {
		t.Errorf("firstNonEmpty() = %q, want %q", got, "a")
	}

	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("firstNonEmpty() = %q, want empty string", got)
	}
}

func TestRandomOIDCValue(t *testing.T) {
	t.Parallel()

	a, err := randomOIDCValue()
	if err != nil {
		t.Fatalf("randomOIDCValue: %v", err)
	}

	if a == "" {
		t.Fatal("randomOIDCValue returned an empty value")
	}

	b, err := randomOIDCValue()
	if err != nil {
		t.Fatalf("randomOIDCValue: %v", err)
	}

	if a == b {
		t.Fatal("randomOIDCValue produced the same value twice")
	}
}
