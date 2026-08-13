package store

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestNewAPIKeyValue(t *testing.T) {
	t.Parallel()

	a, err := newAPIKeyValue()
	if err != nil {
		t.Fatalf("newAPIKeyValue: %v", err)
	}

	if !strings.HasPrefix(a, APIKeyPrefix) {
		t.Fatalf("newAPIKeyValue() = %q, want prefix %q", a, APIKeyPrefix)
	}

	b, err := newAPIKeyValue()
	if err != nil {
		t.Fatalf("newAPIKeyValue: %v", err)
	}

	if a == b {
		t.Fatal("newAPIKeyValue produced the same key twice")
	}
}

func TestHashAPIKey(t *testing.T) {
	t.Parallel()

	hash := hashAPIKey("key-a")

	if got, err := hex.DecodeString(hash); err != nil || len(got) != 32 {
		t.Fatalf("hashAPIKey() = %q, want a 32-byte hex-encoded SHA-256 digest", hash)
	}

	if hashAPIKey("key-a") != hash {
		t.Fatal("hashAPIKey is not deterministic for the same input")
	}

	if hashAPIKey("key-b") == hash {
		t.Fatal("hashAPIKey produced the same digest for two different keys")
	}
}
