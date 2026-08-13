// Command tileserve-go serves versioned map tile pyramids and geo objects
// over HTTP, backed by PostgreSQL and protected by JWT-based authentication.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"nilswitt.dev/tileserve-go/internal/handler"
	"nilswitt.dev/tileserve-go/internal/store"
	"nilswitt.dev/tileserve-go/internal/sync"
	"nilswitt.dev/tileserve-go/internal/tilearchive"
)

// envOrDefault returns the value of the environment variable key, or
// fallback if it is unset or empty.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}

// main parses configuration (flags/env), connects to and migrates the
// database, optionally seeds an initial user, then wires up and starts the
// HTTP server. See the mux.Handle calls below for the route table.
func main() {
	dataRoot := flag.String("data-root", envOrDefault("DATA_ROOT", "./data/overlays"), "directory to serve files from (env DATA_ROOT)")
	jwtSecret := flag.String("jwt-secret", envOrDefault("JWT_SECRET", ""), "secret used to sign and verify JWTs (env JWT_SECRET)")
	dbDSN := flag.String("db-dsn", envOrDefault("DATABASE_URL", "postgres://user:pass@localhost:5432/db"), "postgres connection string, e.g. postgres://user:pass@host:5432/db (env DATABASE_URL)")
	seedUsername := flag.String("seed-username", envOrDefault("SEED_USERNAME", "admin"), "username to create on startup if it doesn't already exist (env SEED_USERNAME)")
	seedPassword := flag.String("seed-password", envOrDefault("SEED_PASSWORD", "admin"), "password for -seed-username (env SEED_PASSWORD)")
	port := flag.String("port", envOrDefault("PORT", "80"), "port to listen on (env PORT)")

	flag.Parse()

	if err := run(*dataRoot, *jwtSecret, *dbDSN, *seedUsername, *seedPassword, *port); err != nil {
		log.Fatal(err)
	}
}

// run wires up storage and the HTTP server and blocks until the server
// exits. It returns an error instead of calling log.Fatal directly so that
// deferred cleanup (closing the store) always runs.
func run(dataRoot, jwtSecret, dbDSN, seedUsername, seedPassword, port string) error {
	if jwtSecret == "" || dbDSN == "" {
		return errors.New("jwt-secret and db-dsn are both required")
	}

	secret := []byte(jwtSecret)

	// ctx is canceled on SIGINT/SIGTERM, giving the sync manager and the
	// HTTP server a chance to shut down cleanly (see the goroutine after
	// srv is constructed below) rather than being killed mid-request or
	// mid-sync.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.NewStore(ctx, dbDSN)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer st.Close()

	if err := st.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	if seedUsername != "" && seedPassword != "" {
		if err := st.SeedUser(ctx, seedUsername, seedPassword); err != nil {
			return fmt.Errorf("seed user: %w", err)
		}
	}

	// Backfilling missing index.json files is best-effort and independent of
	// serving traffic (see EnsureTileIndexes) — run it in the background
	// rather than delaying server startup on a full data-root filesystem walk.
	go func() {
		if err := tilearchive.EnsureTileIndexes(dataRoot); err != nil {
			log.Printf("backfill tile indexes: %v", err)
		}
	}()

	// The sync manager starts/stops one background puller goroutine per
	// enabled sync_remotes row (see internal/sync.Manager); it reconciles
	// against the database periodically, so remotes added/edited/disabled
	// via the /sync/remotes API take effect without a restart.
	syncManager := sync.NewManager(st, dataRoot)
	go syncManager.Start(ctx)

	mux := http.NewServeMux()
	// GET /healthz: liveness probe, always returns 200 "ok".
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	// GET /version: build metadata (commit, and tag/version if tagged), public.
	mux.HandleFunc("/version", handler.VersionHandler())
	// GET /login: serves the login HTML page. POST /login: exchanges
	// username/password for a JWT and a refresh token.
	mux.HandleFunc("/login", handler.LoginHandler(secret, st))
	// GET /login.js: serves the login page's script.
	mux.HandleFunc("/login.js", handler.LoginScriptHandler())
	// POST /refresh: exchanges a refresh token for a new JWT and refresh token.
	mux.HandleFunc("/refresh", handler.RefreshHandler(secret, st))
	// GET /ui/: serves the self-contained management UI (public, unauthenticated).
	mux.HandleFunc("/ui/", handler.UIHandler())
	// GET /openapi.yaml: serves the OpenAPI 3.0 spec (public, unauthenticated).
	mux.HandleFunc("/openapi.yaml", handler.OpenAPIHandler())
	// GET /maps, POST /maps: list maps visible to the caller / create a map.
	// RequireAuth/OptionalAuth accept both a login JWT and an API key JWT
	// (see handler.parseBearerToken) — st satisfies
	// handler.apiKeySigningKeyResolver.
	mux.Handle("/maps", handler.RequireAuth(secret, st, handler.MapsCollectionHandler(st)))
	// /maps/{id}, /maps/{id}/upload, /maps/{id}/versions,
	// /maps/{id}/permissions[/{username}], /maps/{id}/version/{v}[/bounds|...]:
	// see handler.MapsItemHandler for the full per-route breakdown.
	//
	// OptionalAuth, not RequireAuth: a map's version file serving route may
	// be reachable without a token at all if that map has anonymousAllowed
	// set — MapsItemHandler enforces auth itself on every other route.
	mux.Handle("/maps/", handler.OptionalAuth(secret, st, handler.MapsItemHandler(st, dataRoot)))
	// GET /users, POST /users: list users / create a user (admin-only).
	mux.Handle("/users", handler.RequireAuth(secret, st, handler.UsersCollectionHandler(st)))
	// PUT /users/{username}, DELETE /users/{username}: update / delete a user
	// (admin-only), plus /users/{username}/api-keys[/{id}] (also admin-only).
	mux.Handle("/users/", handler.RequireAuth(secret, st, handler.UserItemHandler(st)))
	// GET /sync/remotes, POST /sync/remotes: list / register remotes to
	// pull a full mirror from (admin-only).
	mux.Handle("/sync/remotes", handler.RequireAuth(secret, st, handler.SyncRemotesCollectionHandler(st)))
	// GET/PUT/DELETE /sync/remotes/{id}, POST /sync/remotes/{id}/trigger
	// (admin-only).
	mux.Handle("/sync/remotes/", handler.RequireAuth(secret, st, handler.SyncRemoteItemHandler(st, syncManager)))
	// POST /keys/generate: server-side RSA key pair generation convenience
	// for the admin UI (admin-only, nothing persisted).
	mux.Handle("/keys/generate", handler.RequireAuth(secret, st, handler.GenerateKeyPairHandler(st)))

	addr := ":" + port
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
		// ReadHeaderTimeout guards against slow-loris style connections that
		// trickle in headers without ever completing a request. The other
		// timeouts are deliberately generous: tile archive uploads and large
		// tile pyramid downloads are legitimate long-running transfers.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	// On shutdown (ctx canceled by SIGINT/SIGTERM), stop accepting new sync
	// work and let the HTTP server drain in-flight requests before exiting.
	// A sync worker's in-flight step is safe to abandon mid-way (see
	// internal/sync.pullVersion's crash-safe ordering), so Stop need not be
	// awaited before shutting down the server.
	go func() {
		<-ctx.Done()
		syncManager.Stop()

		_ = srv.Shutdown(context.Background())
	}()

	log.Printf("tileserve-go listening on %s", addr)

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}
