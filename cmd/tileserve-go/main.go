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
	"nilswitt.dev/tileserve-go/internal/handler/auth"
	"nilswitt.dev/tileserve-go/internal/handler/auth/oidc"
	"nilswitt.dev/tileserve-go/internal/ldapauth"
	"nilswitt.dev/tileserve-go/internal/serverkey"
	"nilswitt.dev/tileserve-go/internal/store"
	"nilswitt.dev/tileserve-go/internal/sync"
	"nilswitt.dev/tileserve-go/internal/tilearchive"
)

type ApplicationConfig struct {
	DataRoot string
	KeysDir  string

	JWTSecret string

	DBDSN string

	SeedUsername string
	SeedPassword string

	Port string

	OIDC auth.ApplicationOIDCConfig

	LDAP auth.ApplicationLDAPConfig
}

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
	keysDir := flag.String("keys-dir", envOrDefault("KEYS_DIR", "./data/keys"), "directory for storing key pairs (env KEYS_DIR)")
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

	config := ApplicationConfig{
		DataRoot:     *dataRoot,
		KeysDir:      *keysDir,
		DBDSN:        *dbDSN,
		SeedUsername: *seedUsername,
		SeedPassword: *seedPassword,
		Port:         *port,
		JWTSecret:    *jwtSecret,
		OIDC: auth.ApplicationOIDCConfig{
			IssuerURL:    *oidcIssuerURL,
			ClientID:     *oidcClientID,
			ClientSecret: *oidcClientSecret,
			RedirectURL:  *oidcRedirectURL,
		},
		LDAP: auth.ApplicationLDAPConfig{
			URL:                *ldapURL,
			BindDN:             *ldapBindDN,
			BindPassword:       *ldapBindPassword,
			BaseDN:             *ldapBaseDN,
			UserFilter:         *ldapUserFilter,
			CACertFile:         *ldapCACertFile,
			StartTLS:           *ldapStartTLS,
			InsecureSkipVerify: *ldapInsecureSkipVerify,
			Debug:              *ldapDebug,
		},
	}

	if err := run(&config); err != nil {
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

func run(config *ApplicationConfig) error {
	if config.DBDSN == "" {
		return errors.New("db-dsn is required")
	}

	secret := []byte(config.JWTSecret)

	// ctx is canceled on SIGINT/SIGTERM, giving the sync manager and the
	// HTTP server a chance to shut down cleanly (see the goroutine after
	// srv is constructed below) rather than being killed mid-request or
	// mid-sync.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := serverkey.EnsureKeyPair(config.KeysDir); err != nil {
		return fmt.Errorf("ensure server key pair: %w", err)
	}

	serverPrivateKey, err := serverkey.LoadPrivateKey(config.KeysDir)
	if err != nil {
		return fmt.Errorf("load server key pair: %w", err)
	}

	st, err := initStore(ctx, config.DBDSN, config.SeedUsername, config.SeedPassword)
	if err != nil {
		return err
	}
	defer st.Close()

	// Backfilling missing index.json files is best-effort and independent of
	// serving traffic (see EnsureTileIndexes) — run it in the background
	// rather than delaying server startup on a full data-root filesystem walk.
	go func() {
		if err := tilearchive.EnsureTileIndexes(config.DataRoot); err != nil {
			log.Printf("backfill tile indexes: %v", err)
		}
	}()

	syncManager := sync.NewManager(st, config.DataRoot, serverPrivateKey)
	go syncManager.Start(ctx)

	oidcAuth, ldapAuth, err := newAuthenticators(ctx, config.LDAP, config.OIDC)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/version", handler.VersionHandler())
	mux.HandleFunc("/login", handler.LoginHandler(secret, st, ldapAuth))
	mux.HandleFunc("/login.js", handler.LoginScriptHandler())
	mux.HandleFunc("/refresh", handler.RefreshHandler(secret, st))
	mux.HandleFunc("/auth/methods", oidc.AuthMethodsHandler(oidcAuth != nil))

	if oidcAuth != nil {
		mux.HandleFunc("/login/oidc", oidc.LoginHandler(oidcAuth))
		mux.HandleFunc("/login/oidc/callback", oidc.CallbackHandler(oidcAuth, secret, st))
	}

	guardAuth := func(h http.Handler) http.Handler {
		return handler.AuthMiddleware(secret, st, h, true)
	}

	checkAuth := func(h http.Handler) http.Handler {
		return handler.AuthMiddleware(secret, st, h, false)
	}

	mux.HandleFunc("/ui/", handler.UIHandler())
	mux.HandleFunc("/openapi.yaml", handler.OpenAPIHandler())
	mux.Handle("/maps", guardAuth(handler.MapsCollectionHandler(st)))
	mux.Handle("/maps/", checkAuth(handler.MapsItemHandler(st, config.DataRoot)))

	mux.Handle("/users", guardAuth(handler.UsersCollectionHandler(st)))
	mux.Handle("/users/", guardAuth(handler.UserItemHandler(st)))
	mux.Handle("/sync/remotes", guardAuth(handler.SyncRemotesCollectionHandler(st)))
	mux.Handle("/sync/remotes/", guardAuth(handler.SyncRemoteItemHandler(st, syncManager)))
	mux.Handle("/keys/generate", guardAuth(handler.GenerateKeyPairHandler(st)))
	mux.Handle("/server/public-key", guardAuth(handler.ServerPublicKeyHandler(st, config.KeysDir)))
	mux.Handle("/audit-logs", guardAuth(handler.AuditLogsCollectionHandler(st)))
	mux.Handle("/permissions", guardAuth(handler.PermissionsCollectionHandler(st)))

	addr := ":" + config.Port
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

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

func initStore(ctx context.Context, dbDSN, seedUsername, seedPassword string) (*store.Store, error) {
	st, err := store.NewStore(ctx, dbDSN)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}

	if err := st.Migrate(ctx); err != nil {
		st.Close()

		return nil, fmt.Errorf("migrate: %w", err)
	}

	if seedUsername != "" && seedPassword != "" {
		if err := st.SeedUser(ctx, seedUsername, seedPassword); err != nil {
			st.Close()

			return nil, fmt.Errorf("seed user: %w", err)
		}
	}

	return st, nil
}

func newAuthenticators(ctx context.Context, ldapConfig auth.ApplicationLDAPConfig, oidcConfig auth.ApplicationOIDCConfig) (*oidc.Authenticator, *ldapauth.Authenticator, error) {
	oidcAuth, err := auth.NewOIDCAuthenticator(ctx, oidcConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("init oidc: %w", err)
	}

	ldapAuth, err := auth.NewLDAPAuthenticator(ldapConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("init ldap: %w", err)
	}

	return oidcAuth, ldapAuth, nil
}
