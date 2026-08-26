package handler

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"os"
	"path/filepath"

	"nilswitt.dev/tileserve-go/internal/serverkey"
	"nilswitt.dev/tileserve-go/internal/store"
)

// generatedKeyBits is the RSA modulus size used for keys this server
// generates on an admin's behalf, matching the minimum this server itself
// requires when registering a public key (see store.minRSAKeyBits).
const generatedKeyBits = 2048

type keyPairResponse struct {
	PrivateKeyPEM string `json:"privateKeyPem"`
	PublicKeyPEM  string `json:"publicKeyPem"`
}

// GenerateKeyPairHandler serves POST /keys/generate (admin-only): generates
// a fresh RSA key pair server-side and returns both halves PEM-encoded,
// without persisting anything. It exists purely as a convenience so the
// vanilla-JS admin UI doesn't require external tooling (e.g. openssl) to
// bootstrap a key pair when registering a user's API key — the caller is
// responsible for keeping the private key and submitting only the public
// half wherever a key gets registered; this server never stores it. Sync
// remotes don't use this: they all authenticate with this server's own
// persistent key pair instead (see ServerPublicKeyHandler).
func GenerateKeyPairHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r, st) {
			return
		}

		if !requireMethod(w, r, http.MethodPost) {
			return
		}

		key, err := rsa.GenerateKey(rand.Reader, generatedKeyBits)
		if err != nil {
			http.Error(w, "failed to generate key pair", http.StatusInternalServerError)
			return
		}

		privDER, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			http.Error(w, "failed to encode private key", http.StatusInternalServerError)
			return
		}

		pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
		if err != nil {
			http.Error(w, "failed to encode public key", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, keyPairResponse{
			PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})),
			PublicKeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})),
		})
	}
}

type serverPublicKeyResponse struct {
	PublicKeyPEM string `json:"publicKeyPem"`
}

// ServerPublicKeyHandler serves GET /server/public-key (admin-only): returns
// this server's own persistent RSA public key (see internal/serverkey,
// generated once on first startup and reused thereafter), PEM-encoded. An
// admin copies this into another tileserve-go instance's "API keys" form to
// register this server as a sync client there — every sync remote this
// server pulls from is authenticated with this same key (see
// internal/sync.Manager), not a key pair generated per remote.
func ServerPublicKeyHandler(st *store.Store, keysDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(w, r, st) {
			return
		}

		if !requireMethod(w, r, http.MethodGet) {
			return
		}

		pemBytes, err := os.ReadFile(filepath.Join(keysDir, serverkey.PublicKeyFileName)) //nolint:gosec // G304: keysDir is a server-operator-supplied startup flag, not request input
		if err != nil {
			http.Error(w, "failed to read server public key", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, serverPublicKeyResponse{PublicKeyPEM: string(pemBytes)})
	}
}
