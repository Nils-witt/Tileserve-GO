package handler

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"

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
// bootstrap a key pair — the caller (an admin registering a user's API key,
// or this instance's own sync worker registering itself against a remote)
// is responsible for keeping the private key and submitting only the public
// half wherever a key gets registered; this server never stores it.
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
