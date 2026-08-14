// Package sync implements the server-to-server sync puller: a background
// worker per configured remote (see Manager) that periodically pulls a full
// mirror of another tileserve-go instance's maps, versions, and aliases.
package sync

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"nilswitt.dev/tileserve-go/internal/store"
)

// maxArchiveSize caps how much data DownloadArchive will read from a
// remote's version archive, mirroring maxUploadSize's role on the server's
// inbound upload side (internal/handler/upload.go).
const maxArchiveSize = 1 << 30 // 1 GiB

// httpTimeout bounds a single request to a remote instance. Archive
// downloads can legitimately be large and slow, so this is deliberately
// generous, matching the server's own request timeouts
// (cmd/tileserve-go/main.go).
const httpTimeout = 5 * time.Minute

// apiKeyTokenTTL is how long each outbound JWT this client mints is valid
// for — well under the remote's maxAPIKeyTokenLifetime policy ceiling
// (internal/handler/auth.go), and short enough that minting a fresh one on
// every request (rather than caching/reusing) is simplest and cheap given
// sync's low request volume.
const apiKeyTokenTTL = 5 * time.Minute

// Client talks to one remote tileserve-go instance's REST API using a
// caller-signed API key JWT (see store.CreateAPIKey), decoding responses
// directly into the remote's own JSON types (store.MapRecord, etc.) rather
// than separate DTOs — valid specifically because the remote is guaranteed
// to be another tileserve-go instance running the same code.
type Client struct {
	baseURL    string
	keyID      uuid.UUID
	privateKey *rsa.PrivateKey
	http       *http.Client
}

// NewClient returns a Client for the remote instance at baseURL,
// authenticating every request with a freshly-signed RS256 JWT (see
// signToken) naming keyID as its `kid` and signed with privateKeyPEM — the
// private half of the key pair whose public half was registered as API key
// keyID on the remote. It returns an error if privateKeyPEM doesn't parse.
func NewClient(baseURL string, keyID uuid.UUID, privateKeyPEM string) (*Client, error) {
	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(privateKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("parse sync remote private key: %w", err)
	}

	return &Client{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		keyID:      keyID,
		privateKey: privateKey,
		http:       &http.Client{Timeout: httpTimeout},
	}, nil
}

// signToken mints a fresh, short-lived RS256 JWT identifying this client's
// registered API key via the `kid` header. The `sub` claim is deliberately
// left unset: the remote never trusts it (identity comes from kid -> DB
// lookup, see handler.parseBearerToken), so setting it would only mislead.
func (c *Client) signToken() (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(apiKeyTokenTTL)),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = c.keyID.String()

	return token.SignedString(c.privateKey)
}

// newRequest builds an authenticated request for path (relative to
// baseURL).
func (c *Client) newRequest(ctx context.Context, path string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", path, err)
	}

	token, err := c.signToken()
	if err != nil {
		return nil, fmt.Errorf("sign api key jwt for %s: %w", path, err)
	}

	req.Header.Set("Authorization", "Bearer "+token)

	return req, nil
}

// getJSON issues an authenticated GET to path and decodes the JSON response
// body into v.
func (c *Client) getJSON(ctx context.Context, path string, v any) error {
	req, err := c.newRequest(ctx, path)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("request %s: unexpected status %d", path, resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("decode response for %s: %w", path, err)
	}

	return nil
}

// ListMaps returns every map visible to the client's API key on the remote.
func (c *Client) ListMaps(ctx context.Context) ([]store.MapRecord, error) {
	var maps []store.MapRecord
	if err := c.getJSON(ctx, "/maps", &maps); err != nil {
		return nil, err
	}

	return maps, nil
}

// ListVersions returns mapID's version history on the remote.
func (c *Client) ListVersions(ctx context.Context, mapID uuid.UUID) ([]store.MapVersionRecord, error) {
	var versions []store.MapVersionRecord
	if err := c.getJSON(ctx, "/maps/"+mapID.String()+"/versions", &versions); err != nil {
		return nil, err
	}

	return versions, nil
}

// ListAliases returns mapID's version aliases on the remote.
func (c *Client) ListAliases(ctx context.Context, mapID uuid.UUID) ([]store.MapVersionAlias, error) {
	var aliases []store.MapVersionAlias
	if err := c.getJSON(ctx, "/maps/"+mapID.String()+"/aliases", &aliases); err != nil {
		return nil, err
	}

	return aliases, nil
}

// ListGeoObjects returns every geo object tied to mapID's version on the
// remote.
func (c *Client) ListGeoObjects(ctx context.Context, mapID uuid.UUID, version string) ([]store.GeoObjectRecord, error) {
	var objs []store.GeoObjectRecord

	path := "/maps/" + mapID.String() + "/version/" + url.PathEscape(version) + "/geo-objects"
	if err := c.getJSON(ctx, path, &objs); err != nil {
		return nil, err
	}

	return objs, nil
}

// DownloadArchive streams mapID's version archive
// (GET .../version/{version}/archive) from the remote to a fresh temp file,
// returning its path. On success the caller is responsible for removing it.
// The download is capped at maxArchiveSize, mirroring the inbound upload
// cap on the server side.
func (c *Client) DownloadArchive(ctx context.Context, mapID uuid.UUID, version string) (tmpPath string, err error) {
	path := "/maps/" + mapID.String() + "/version/" + url.PathEscape(version) + "/archive"

	req, err := c.newRequest(ctx, path)
	if err != nil {
		return "", err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("request %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("request %s: unexpected status %d", path, resp.StatusCode)
	}

	return writeArchiveToTemp(resp.Body)
}

// writeArchiveToTemp copies r to a fresh temp file, capped at
// maxArchiveSize+1 (so exceeding the cap is detected without reading an
// unbounded amount of attacker/misbehaving-remote-controlled data). On
// success the caller is responsible for removing the returned path.
func writeArchiveToTemp(r io.Reader) (tmpPath string, err error) {
	tmpFile, err := os.CreateTemp("", "tileserve-sync-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer func() { _ = tmpFile.Close() }()

	written, err := io.Copy(tmpFile, io.LimitReader(r, maxArchiveSize+1))
	if err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("download archive: %w", err)
	}

	if written > maxArchiveSize {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("archive exceeds max size of %d bytes", maxArchiveSize)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("close temp file: %w", err)
	}

	return tmpFile.Name(), nil
}
