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
	"nilswitt.dev/tileserve-go/internal/ldapauth"
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
	if len(os.Args) > 1 && os.Args[1] == "status" {
		statusCmd(os.Args[2:])
		return
	}

	dataRoot := flag.String("data-root", envOrDefault("DATA_ROOT", "./data/overlays"), "directory to serve files from (env DATA_ROOT)")
	jwtSecret := flag.String("jwt-secret", envOrDefault("JWT_SECRET", ""), "secret used to sign and verify JWTs (env JWT_SECRET)")
	dbDSN := flag.String("db-dsn", envOrDefault("DATABASE_URL", "postgres://user:pass@localhost:5432/db"), "postgres connection string, e.g. postgres://user:pass@host:5432/db (env DATABASE_URL)")
	seedUsername := flag.String("seed-username", envOrDefault("SEED_USERNAME", "admin"), "username to create on startup if it doesn't already exist (env SEED_USERNAME)")
	seedPassword := flag.String("seed-password", envOrDefault("SEED_PASSWORD", "admin"), "password for -seed-username (env SEED_PASSWORD)")
	port := flag.String("port", envOrDefault("PORT", "80"), "port to listen on (env PORT)")
	oidcIssuerURL := flag.String("oidc-issuer-url", envOrDefault("OIDC_ISSUER_URL", ""), "OpenID Connect issuer URL; set together with -oidc-client-id, -oidc-client-secret and -oidc-redirect-url to enable SSO login (env OIDC_ISSUER_URL)")
	oidcClientID := flag.String("oidc-client-id", envOrDefault("OIDC_CLIENT_ID", ""), "OpenID Connect client id (env OIDC_CLIENT_ID)")
	oidcClientSecret := flag.String("oidc-client-secret", envOrDefault("OIDC_CLIENT_SECRET", ""), "OpenID Connect client secret (env OIDC_CLIENT_SECRET)")
	oidcRedirectURL := flag.String("oidc-redirect-url", envOrDefault("OIDC_REDIRECT_URL", ""), "OpenID Connect redirect URL, must exactly match what's registered with the provider, e.g. https://tiles.example.com/login/oidc/callback (env OIDC_REDIRECT_URL)")
	ldapURL := flag.String("ldap-url", envOrDefault("LDAP_URL", ""), "LDAP server URL, e.g. ldaps://ldap.example.com:636; set together with -ldap-base-dn and -ldap-user-filter to let /login fall back to LDAP bind authentication for accounts not known locally (env LDAP_URL)")
	ldapBindDN := flag.String("ldap-bind-dn", envOrDefault("LDAP_BIND_DN", ""), "DN of the service account used to search for a user's entry; leave unset to search anonymously (env LDAP_BIND_DN)")
	ldapBindPassword := flag.String("ldap-bind-password", envOrDefault("LDAP_BIND_PASSWORD", ""), "password for -ldap-bind-dn (env LDAP_BIND_PASSWORD)")
	ldapBaseDN := flag.String("ldap-base-dn", envOrDefault("LDAP_BASE_DN", ""), "search base for resolving a username to a directory entry, e.g. ou=people,dc=example,dc=com (env LDAP_BASE_DN)")
	ldapUserFilter := flag.String("ldap-user-filter", envOrDefault("LDAP_USER_FILTER", "(uid=%s)"), `LDAP filter used to find a user's entry below -ldap-base-dn, with a "%s" placeholder for the username, e.g. (sAMAccountName=%s) for Active Directory (env LDAP_USER_FILTER)`)
	ldapStartTLS := flag.Bool("ldap-start-tls", envOrDefault("LDAP_START_TLS", "") == "true", "upgrade a plain ldap:// connection with StartTLS before binding; ignored for an ldaps:// URL (env LDAP_START_TLS)")
	ldapInsecureSkipVerify := flag.Bool("ldap-insecure-skip-verify", envOrDefault("LDAP_INSECURE_SKIP_VERIFY", "") == "true", "skip verification of the LDAP server's TLS certificate, for ldaps:// or -ldap-start-tls; insecure, only use if the certificate can't otherwise be trusted (env LDAP_INSECURE_SKIP_VERIFY)")
	ldapCACertFile := flag.String("ldap-ca-cert-file", envOrDefault("LDAP_CA_CERT_FILE", ""), "path to a PEM-encoded CA certificate (or bundle) used to verify the LDAP server's TLS certificate, for ldaps:// or -ldap-start-tls, instead of the system trust store; ignored if -ldap-insecure-skip-verify is set (env LDAP_CA_CERT_FILE)")
	ldapDebug := flag.Bool("ldap-debug", envOrDefault("LDAP_DEBUG", "") == "true", "log each step of LDAP authentication (bind/search attempts, resolved DN, success/failure), including the username of every login attempt; off by default since that's per-attempt log volume (env LDAP_DEBUG)")

	flag.Parse()

	if err := run(*dataRoot, *jwtSecret, *dbDSN, *seedUsername, *seedPassword, *port, *oidcIssuerURL, *oidcClientID, *oidcClientSecret, *oidcRedirectURL, *ldapURL, *ldapBindDN, *ldapBindPassword, *ldapBaseDN, *ldapUserFilter, *ldapCACertFile, *ldapStartTLS, *ldapInsecureSkipVerify, *ldapDebug); err != nil {
		log.Fatal(err)
	}
}

// statusCmd implements `tileserve-go status`, a lightweight check for
// whether a tileserve-go server is already listening locally: it hits
// /healthz on the given port and reports up/down via exit code (0 up, 1
// down), without touching the database or any other server dependency.
func statusCmd(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	port := fs.String("port", envOrDefault("PORT", "80"), "port the local server is expected to listen on (env PORT)")
	_ = fs.Parse(args)

	url := "http://127.0.0.1:" + *port + "/healthz"
	client := http.Client{Timeout: 2 * time.Second}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		fmt.Printf("tileserve-go is not running on port %s: %v\n", *port, err)
		os.Exit(1)
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("tileserve-go is not running on port %s: %v\n", *port, err)
		os.Exit(1)
	}

	statusCode := resp.StatusCode
	if err := resp.Body.Close(); err != nil {
		fmt.Printf("tileserve-go is not running on port %s: %v\n", *port, err)
		os.Exit(1)
	}

	if statusCode != http.StatusOK {
		fmt.Printf("tileserve-go is not running on port %s: /healthz returned %d\n", *port, statusCode)
		os.Exit(1)
	}

	fmt.Printf("tileserve-go is running on port %s\n", *port)
}

// run wires up storage and the HTTP server and blocks until the server
// exits. It returns an error instead of calling log.Fatal directly so that
// deferred cleanup (closing the store) always runs.
func run(dataRoot, jwtSecret, dbDSN, seedUsername, seedPassword, port, oidcIssuerURL, oidcClientID, oidcClientSecret, oidcRedirectURL, ldapURL, ldapBindDN, ldapBindPassword, ldapBaseDN, ldapUserFilter, ldapCACertFile string, ldapStartTLS, ldapInsecureSkipVerify, ldapDebug bool) error {
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

	oidcAuth, ldapAuth, err := newAuthenticators(ctx, oidcIssuerURL, oidcClientID, oidcClientSecret, oidcRedirectURL, ldapURL, ldapBindDN, ldapBindPassword, ldapBaseDN, ldapUserFilter, ldapCACertFile, ldapStartTLS, ldapInsecureSkipVerify, ldapDebug)
	if err != nil {
		return err
	}

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
	mux.HandleFunc("/login", handler.LoginHandler(secret, st, ldapAuth))
	// GET /login.js: serves the login page's script.
	mux.HandleFunc("/login.js", handler.LoginScriptHandler())
	// POST /refresh: exchanges a refresh token for a new JWT and refresh token.
	mux.HandleFunc("/refresh", handler.RefreshHandler(secret, st))
	// GET /auth/methods: reports which login methods are available (public),
	// so the login pages' script knows whether to show the SSO button.
	mux.HandleFunc("/auth/methods", handler.AuthMethodsHandler(oidcAuth != nil))

	if oidcAuth != nil {
		// GET /login/oidc: starts the OpenID Connect login redirect.
		mux.HandleFunc("/login/oidc", handler.OIDCLoginHandler(oidcAuth))
		// GET /login/oidc/callback: the provider's redirect back; on success
		// issues a normal login JWT and refresh token, same as /login.
		mux.HandleFunc("/login/oidc/callback", handler.OIDCCallbackHandler(oidcAuth, secret, st))
	}
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
	// GET /audit-logs: lists recorded audit entries (admin-only).
	mux.Handle("/audit-logs", handler.RequireAuth(secret, st, handler.AuditLogsCollectionHandler(st)))

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

// newAuthenticators builds the optional OIDC and LDAP authenticators from
// their respective -oidc-*/-ldap-* settings (see newOIDCAuthenticator and
// newLDAPAuthenticator), bundled into one call so run only has a single
// error check to make for both.
func newAuthenticators(ctx context.Context, oidcIssuerURL, oidcClientID, oidcClientSecret, oidcRedirectURL, ldapURL, ldapBindDN, ldapBindPassword, ldapBaseDN, ldapUserFilter, ldapCACertFile string, ldapStartTLS, ldapInsecureSkipVerify, ldapDebug bool) (*handler.OIDCAuthenticator, *ldapauth.Authenticator, error) {
	oidcAuth, err := newOIDCAuthenticator(ctx, oidcIssuerURL, oidcClientID, oidcClientSecret, oidcRedirectURL)
	if err != nil {
		return nil, nil, fmt.Errorf("init oidc: %w", err)
	}

	ldapAuth, err := newLDAPAuthenticator(ldapURL, ldapBindDN, ldapBindPassword, ldapBaseDN, ldapUserFilter, ldapCACertFile, ldapStartTLS, ldapInsecureSkipVerify, ldapDebug)
	if err != nil {
		return nil, nil, fmt.Errorf("init ldap: %w", err)
	}

	return oidcAuth, ldapAuth, nil
}

// newOIDCAuthenticator builds the OIDC authenticator from the four
// -oidc-* settings. They must either all be empty (feature off — nil, nil is
// returned and /login/oidc[/callback] aren't registered at all, see run
// above) or all be set (feature on); a partial set is almost certainly a
// misconfiguration, so it's rejected outright rather than silently running
// with SSO half-enabled.
func newOIDCAuthenticator(ctx context.Context, issuerURL, clientID, clientSecret, redirectURL string) (*handler.OIDCAuthenticator, error) {
	if issuerURL == "" && clientID == "" && clientSecret == "" && redirectURL == "" {
		return nil, nil
	}

	if issuerURL == "" || clientID == "" || clientSecret == "" || redirectURL == "" {
		return nil, errors.New("oidc-issuer-url, oidc-client-id, oidc-client-secret, and oidc-redirect-url must all be set together to enable OpenID Connect login")
	}

	return handler.NewOIDCAuthenticator(ctx, issuerURL, clientID, clientSecret, redirectURL)
}

// newLDAPAuthenticator builds the LDAP authenticator from the -ldap-*
// settings. LDAP login is off (nil, nil) unless -ldap-url is set; once it
// is, -ldap-base-dn is also required (-ldap-bind-dn/-ldap-bind-password,
// -ldap-start-tls, -ldap-ca-cert-file, -ldap-insecure-skip-verify, and
// -ldap-debug are optional, and -ldap-user-filter always has a default).
func newLDAPAuthenticator(url, bindDN, bindPassword, baseDN, userFilter, caCertFile string, startTLS, insecureSkipVerify, debug bool) (*ldapauth.Authenticator, error) {
	if url == "" {
		return nil, nil
	}

	if baseDN == "" {
		return nil, errors.New("ldap-base-dn is required when ldap-url is set")
	}

	return ldapauth.NewAuthenticator(ldapauth.Config{
		URL:                url,
		BindDN:             bindDN,
		BindPassword:       bindPassword,
		BaseDN:             baseDN,
		UserFilter:         userFilter,
		StartTLS:           startTLS,
		InsecureSkipVerify: insecureSkipVerify,
		CACertFile:         caCertFile,
		Debug:              debug,
	})
}
