package store

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
)

// testRSAPublicKeyPEM generates a fresh RSA key of the given size and
// returns its PKIX-encoded public key PEM.
func testRSAPublicKeyPEM(t *testing.T, bits int) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func TestValidateRSAPublicKeyPEM(t *testing.T) {
	t.Parallel()

	valid := testRSAPublicKeyPEM(t, 2048)
	tooSmall := testRSAPublicKeyPEM(t, 1024)

	tests := []struct {
		name    string
		pemStr  string
		wantErr bool
	}{
		{name: "valid 2048-bit key", pemStr: valid, wantErr: false},
		{name: "empty string", pemStr: "", wantErr: true},
		{name: "not PEM at all", pemStr: "not a pem block", wantErr: true},
		{name: "key too small", pemStr: tooSmall, wantErr: true},
		{
			name: "PEM block that isn't a public key",
			pemStr: string(pem.EncodeToMemory(&pem.Block{
				Type:  "PUBLIC KEY",
				Bytes: []byte("not a valid PKIX-encoded key"),
			})),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateRSAPublicKeyPEM(tc.pemStr)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateRSAPublicKeyPEM(%q) error = %v, wantErr %v", tc.name, err, tc.wantErr)
			}
		})
	}
}
