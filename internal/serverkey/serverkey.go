// Package serverkey ensures this tileserve-go instance has a persistent RSA
// key pair of its own, stored under the configured keys directory.
package serverkey

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

// keyBits is the RSA modulus size generated for this server's own key pair.
// Larger than generatedKeyBits in internal/handler/keypair.go (which
// generates keys on an admin's behalf for short-lived API key registration)
// since this key pair is meant to persist for the server's lifetime.
const keyBits = 4096

// PrivateKeyFileName and PublicKeyFileName are the file names EnsureKeyPair
// reads and writes within the keys directory.
const (
	PrivateKeyFileName = "server.key"
	PublicKeyFileName  = "server.pub"
)

// EnsureKeyPair makes sure dir contains a server.key/server.pub RSA key
// pair, creating dir and generating a fresh 4096-bit pair (PKCS8 private
// key, PKIX public key, both PEM-encoded) if server.key doesn't already
// exist. An existing server.key is left untouched, so this is safe to call
// on every startup.
func EnsureKeyPair(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create keys dir: %w", err)
	}

	privatePath := filepath.Join(dir, PrivateKeyFileName)

	if _, err := os.Stat(privatePath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", privatePath, err)
	}

	key, err := rsa.GenerateKey(rand.Reader, keyBits)
	if err != nil {
		return fmt.Errorf("generate server key pair: %w", err)
	}

	privDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("encode server private key: %w", err)
	}

	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return fmt.Errorf("encode server public key: %w", err)
	}

	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})
	if err := os.WriteFile(privatePath, privatePEM, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", privatePath, err)
	}

	publicPath := filepath.Join(dir, PublicKeyFileName)

	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	if err := os.WriteFile(publicPath, publicPEM, 0o644); err != nil { //nolint:gosec // G306: a public key is meant to be readable, not a secret
		return fmt.Errorf("write %s: %w", publicPath, err)
	}

	return nil
}
