package serverkey

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureKeyPairGeneratesWhenMissing(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "keys")

	if err := EnsureKeyPair(dir); err != nil {
		t.Fatalf("EnsureKeyPair: %v", err)
	}

	privatePEM, err := os.ReadFile(filepath.Join(dir, PrivateKeyFileName))
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}

	block, _ := pem.Decode(privatePEM)
	if block == nil || block.Type != "PRIVATE KEY" {
		t.Fatalf("private key PEM block: got %+v", block)
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}

	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		t.Fatalf("private key type: got %T, want *rsa.PrivateKey", parsed)
	}

	if bitLen := key.Size() * 8; bitLen != keyBits {
		t.Fatalf("key size: got %d bits, want %d", bitLen, keyBits)
	}

	publicPEM, err := os.ReadFile(filepath.Join(dir, PublicKeyFileName))
	if err != nil {
		t.Fatalf("read public key: %v", err)
	}

	pubBlock, _ := pem.Decode(publicPEM)
	if pubBlock == nil || pubBlock.Type != "PUBLIC KEY" {
		t.Fatalf("public key PEM block: got %+v", pubBlock)
	}

	if _, err := x509.ParsePKIXPublicKey(pubBlock.Bytes); err != nil {
		t.Fatalf("parse public key: %v", err)
	}
}

func TestLoadPrivateKeyMatchesGeneratedKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	if err := EnsureKeyPair(dir); err != nil {
		t.Fatalf("EnsureKeyPair: %v", err)
	}

	privatePEM, err := os.ReadFile(filepath.Join(dir, PrivateKeyFileName))
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}

	block, _ := pem.Decode(privatePEM)

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}

	want, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		t.Fatalf("private key type: got %T, want *rsa.PrivateKey", parsed)
	}

	got, err := LoadPrivateKey(dir)
	if err != nil {
		t.Fatalf("LoadPrivateKey: %v", err)
	}

	if !got.Equal(want) {
		t.Fatal("LoadPrivateKey returned a different key than EnsureKeyPair generated")
	}
}

func TestLoadPrivateKeyMissingFile(t *testing.T) {
	t.Parallel()

	if _, err := LoadPrivateKey(t.TempDir()); err == nil {
		t.Fatal("LoadPrivateKey with no server.key present should return an error")
	}
}

func TestEnsureKeyPairLeavesExistingKeyAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	if err := EnsureKeyPair(dir); err != nil {
		t.Fatalf("EnsureKeyPair (first call): %v", err)
	}

	before, err := os.ReadFile(filepath.Join(dir, PrivateKeyFileName))
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}

	if err := EnsureKeyPair(dir); err != nil {
		t.Fatalf("EnsureKeyPair (second call): %v", err)
	}

	after, err := os.ReadFile(filepath.Join(dir, PrivateKeyFileName))
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}

	if string(before) != string(after) {
		t.Fatal("EnsureKeyPair regenerated an existing private key")
	}
}
