// Package sync implements the server-to-server sync puller: a background
// worker per configured remote (see Manager) that periodically pulls a full
// mirror of another tileserve-go instance's maps, versions, and aliases.
package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

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

// Client talks to one remote tileserve-go instance's REST API using an API
// key (see store.CreateAPIKey), decoding responses directly into the
// remote's own JSON types (store.MapRecord, etc.) rather than separate DTOs
// — valid specifically because the remote is guaranteed to be another
// tileserve-go instance running the same code.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewClient returns a Client for the remote instance at baseURL,
// authenticating every request with apiKey.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: httpTimeout},
	}
}

// newRequest builds an authenticated request for path (relative to
// baseURL).
func (c *Client) newRequest(ctx context.Context, path string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", path, err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)

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
