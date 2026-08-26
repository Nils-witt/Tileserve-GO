package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// ErrInvalidCredentials is returned when a login's username/password pair does not match.
var ErrInvalidCredentials = errors.New("invalid credentials")

// cacheTTL bounds how stale a cached map/permission lookup may be. It's the
// window between a permission or visibility change in postgres and that
// change taking effect for cached reads (e.g. on the tile-serving hot path).
const (
	cacheTTL      = 15 * time.Second
	mapVersionTTL = 30 * time.Minute
)

type mapPermKey struct {
	mapID    uuid.UUID
	username string
}

// apiKeyScopeKey caches one API key's scope entry for one map.
type apiKeyScopeKey struct {
	apiKeyID uuid.UUID
	mapUUID  uuid.UUID
}

// Store is the PostgreSQL-backed persistence layer for tileserve-go.
type Store struct {
	pool                *pgxpool.Pool
	mapCache            *ttlCache[uuid.UUID, MapRecord]
	currentVersionCache *ttlCache[uuid.UUID, string]
	permsCache          *ttlCache[string, Permissions]
	mapPermCache        *ttlCache[mapPermKey, MapPermission]
	mapAliasCache       *ttlCache[mapAliasKey, string]
	apiKeyCache         *ttlCache[uuid.UUID, apiKeySigningKey]
	apiKeyScopeCache    *ttlCache[apiKeyScopeKey, apiKeyScopeEntry]
}

// NewStore opens a connection pool to the postgres database at dsn and
// verifies it is reachable with a ping.
func NewStore(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}
	// Explicit bounds rather than the library defaults: enough headroom for
	// concurrent request handling without letting a traffic spike open an
	// unbounded number of postgres connections.
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return &Store{
		pool:                pool,
		mapCache:            newTTLCache[uuid.UUID, MapRecord](cacheTTL),
		currentVersionCache: newTTLCache[uuid.UUID, string](mapVersionTTL),
		permsCache:          newTTLCache[string, Permissions](cacheTTL),
		mapPermCache:        newTTLCache[mapPermKey, MapPermission](cacheTTL),
		mapAliasCache:       newTTLCache[mapAliasKey, string](cacheTTL),
		apiKeyCache:         newTTLCache[uuid.UUID, apiKeySigningKey](cacheTTL),
		apiKeyScopeCache:    newTTLCache[apiKeyScopeKey, apiKeyScopeEntry](cacheTTL),
	}, nil
}

// Close releases the underlying database connection pool.
func (s *Store) Close() {
	s.pool.Close()
}

// Migrate creates the users, maps, map_versions, map_permissions,
// map_version_aliases, and geo_objects tables if they don't exist yet, and
// adds any columns introduced since the tables were first created. It is
// idempotent and safe to run on every startup.
func (s *Store) Migrate(ctx context.Context) error {
	for _, m := range migrationSteps {
		if _, err := s.pool.Exec(ctx, m.sql); err != nil {
			return fmt.Errorf("%s: %w", m.errContext, err)
		}
	}

	return nil
}

// migrationSteps are applied in order by Migrate. Each is a standalone,
// idempotent DDL statement (CREATE TABLE/INDEX IF NOT EXISTS, ALTER TABLE
// ADD COLUMN IF NOT EXISTS), so re-running the full list on every startup is
// safe even once earlier steps have already been applied.
var migrationSteps = []struct {
	sql        string
	errContext string
}{
	{
		errContext: "migrate users table",
		sql: `
			CREATE TABLE IF NOT EXISTS users (
				id            BIGSERIAL PRIMARY KEY,
				username      TEXT NOT NULL UNIQUE,
				password_hash TEXT NOT NULL,
				can_create    BOOLEAN NOT NULL DEFAULT true,
				can_edit      BOOLEAN NOT NULL DEFAULT true,
				can_delete    BOOLEAN NOT NULL DEFAULT true,
				is_admin      BOOLEAN NOT NULL DEFAULT true,
				created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
			)
		`,
	},
	{
		errContext: "migrate users permission columns",
		sql: `
			ALTER TABLE users ADD COLUMN IF NOT EXISTS can_create BOOLEAN NOT NULL DEFAULT true;
			ALTER TABLE users ADD COLUMN IF NOT EXISTS can_edit   BOOLEAN NOT NULL DEFAULT true;
			ALTER TABLE users ADD COLUMN IF NOT EXISTS can_delete BOOLEAN NOT NULL DEFAULT true;
			ALTER TABLE users ADD COLUMN IF NOT EXISTS is_admin   BOOLEAN NOT NULL DEFAULT true;
		`,
	},
	{
		// Defaults to true, like the other global permission columns above:
		// an upgrading deployment's existing editors (can_edit) keep the
		// ability to edit/delete geo objects until an admin narrows it.
		errContext: "migrate users geo object permission columns",
		sql: `
			ALTER TABLE users ADD COLUMN IF NOT EXISTS can_edit_geo_objects   BOOLEAN NOT NULL DEFAULT true;
			ALTER TABLE users ADD COLUMN IF NOT EXISTS can_delete_geo_objects BOOLEAN NOT NULL DEFAULT true;
		`,
	},
	{
		errContext: "migrate maps table",
		sql: `
			CREATE TABLE IF NOT EXISTS maps (
				uuid            UUID PRIMARY KEY,
				name            TEXT NOT NULL,
				current_version TEXT NOT NULL DEFAULT '',
				created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
				updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
				created_by      TEXT NOT NULL,
				updated_by      TEXT NOT NULL
			)
		`,
	},
	{
		errContext: "migrate maps visibility column",
		sql: `
			ALTER TABLE maps ADD COLUMN IF NOT EXISTS visible_to_all     BOOLEAN NOT NULL DEFAULT false;
			ALTER TABLE maps ADD COLUMN IF NOT EXISTS anonymous_allowed  BOOLEAN NOT NULL DEFAULT false;
		`,
	},
	{
		errContext: "migrate map_versions table",
		sql: `
			CREATE TABLE IF NOT EXISTS map_versions (
				map_uuid   UUID NOT NULL REFERENCES maps(uuid) ON DELETE CASCADE,
				version    TEXT NOT NULL,
				created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
				created_by TEXT NOT NULL,
				PRIMARY KEY (map_uuid, version)
			)
		`,
	},
	{
		errContext: "migrate geo_objects table",
		sql: `
			CREATE TABLE IF NOT EXISTS geo_objects (
				uuid        UUID PRIMARY KEY,
				map_uuid    UUID NOT NULL,
				version     TEXT NOT NULL,
				name        TEXT NOT NULL,
				external_id TEXT NOT NULL DEFAULT '',
				latitude    DOUBLE PRECISION NOT NULL,
				longitude   DOUBLE PRECISION NOT NULL,
				street      TEXT NOT NULL DEFAULT '',
				housenumber TEXT NOT NULL DEFAULT '',
				postcode    TEXT NOT NULL DEFAULT '',
				created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
				updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
				created_by  TEXT NOT NULL,
				updated_by  TEXT NOT NULL,
				FOREIGN KEY (map_uuid, version) REFERENCES map_versions(map_uuid, version) ON DELETE CASCADE
			)
		`,
	},
	{
		errContext: "migrate geo_objects city column",
		sql: `
			ALTER TABLE geo_objects ADD COLUMN IF NOT EXISTS city TEXT NOT NULL DEFAULT '';
		`,
	},
	{
		errContext: "migrate geo_objects city_district column",
		sql: `
			ALTER TABLE geo_objects ADD COLUMN IF NOT EXISTS city_district TEXT NOT NULL DEFAULT '';
		`,
	},
	{
		errContext: "migrate geo_objects index",
		sql: `
			CREATE INDEX IF NOT EXISTS idx_geo_objects_map_version ON geo_objects (map_uuid, version);
		`,
	},
	{
		errContext: "migrate map_permissions table",
		sql: `
			CREATE TABLE IF NOT EXISTS map_permissions (
				map_uuid   UUID NOT NULL REFERENCES maps(uuid) ON DELETE CASCADE,
				username   TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,
				can_edit   BOOLEAN NOT NULL DEFAULT false,
				can_delete BOOLEAN NOT NULL DEFAULT false,
				granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
				granted_by TEXT NOT NULL,
				PRIMARY KEY (map_uuid, username)
			)
		`,
	},
	{
		errContext: "migrate map_permissions view column",
		sql: `
			ALTER TABLE map_permissions ADD COLUMN IF NOT EXISTS can_view BOOLEAN NOT NULL DEFAULT false;
		`,
	},
	{
		// Opt-in, like the other per-map grant columns: a per-map grant only
		// ever adds capability, so it defaults to false.
		errContext: "migrate map_permissions geo object columns",
		sql: `
			ALTER TABLE map_permissions ADD COLUMN IF NOT EXISTS can_edit_geo_objects   BOOLEAN NOT NULL DEFAULT false;
			ALTER TABLE map_permissions ADD COLUMN IF NOT EXISTS can_delete_geo_objects BOOLEAN NOT NULL DEFAULT false;
		`,
	},
	{
		errContext: "migrate refresh_tokens table",
		sql: `
			CREATE TABLE IF NOT EXISTS refresh_tokens (
				token_hash TEXT PRIMARY KEY,
				username   TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,
				expires_at TIMESTAMPTZ NOT NULL,
				created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
				revoked_at TIMESTAMPTZ
			)
		`,
	},
	{
		errContext: "migrate map_version_aliases table",
		sql: `
			CREATE TABLE IF NOT EXISTS map_version_aliases (
				map_uuid   UUID NOT NULL REFERENCES maps(uuid) ON DELETE CASCADE,
				alias      TEXT NOT NULL,
				version    TEXT NOT NULL,
				created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
				updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
				created_by TEXT NOT NULL,
				updated_by TEXT NOT NULL,
				PRIMARY KEY (map_uuid, alias),
				FOREIGN KEY (map_uuid, version) REFERENCES map_versions(map_uuid, version) ON DELETE CASCADE
			)
		`,
	},
	{
		errContext: "migrate api_keys table",
		sql: `
			CREATE TABLE IF NOT EXISTS api_keys (
				id             UUID PRIMARY KEY,
				public_key_pem TEXT NOT NULL DEFAULT '',
				username       TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,
				name           TEXT NOT NULL DEFAULT '',
				created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
				created_by     TEXT NOT NULL,
				last_used_at   TIMESTAMPTZ,
				revoked_at     TIMESTAMPTZ
			);
			CREATE INDEX IF NOT EXISTS idx_api_keys_username ON api_keys (username);
		`,
	},
	{
		// Breaking-change replacement of the opaque-secret scheme with per-key
		// RSA public keys (see api_keys.go): the server now only ever stores a
		// caller-generated public key, never a secret of its own. Existing
		// key_hash rows become permanently unusable (public_key_pem defaults
		// to '', which fails to parse) — there is no migration path for
		// pre-existing opaque keys, they must be recreated.
		errContext: "migrate api_keys public key columns",
		sql: `
			ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS public_key_pem TEXT NOT NULL DEFAULT '';
			ALTER TABLE api_keys DROP COLUMN IF EXISTS key_hash;
		`,
	},
	{
		// private_key_pem is this (local) server's own RSA private key, used
		// to sign short-lived JWTs presented to the remote — stored in
		// plaintext (unlike every other secret in this schema, which is
		// one-way hashed) because it must be read back to sign each outbound
		// request. remote_api_key_id is not secret: it's the id of the
		// api_keys row the matching public key was registered as *on the
		// remote*, so it isn't a local foreign key.
		errContext: "migrate sync_remotes table",
		sql: `
			CREATE TABLE IF NOT EXISTS sync_remotes (
				id                UUID PRIMARY KEY,
				name              TEXT NOT NULL,
				base_url          TEXT NOT NULL,
				remote_api_key_id UUID,
				private_key_pem   TEXT NOT NULL DEFAULT '',
				poll_interval_sec INTEGER NOT NULL DEFAULT 300,
				enabled           BOOLEAN NOT NULL DEFAULT true,
				last_sync_at      TIMESTAMPTZ,
				last_sync_status  TEXT NOT NULL DEFAULT '',
				last_sync_error   TEXT NOT NULL DEFAULT '',
				created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
				updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
				created_by        TEXT NOT NULL,
				updated_by        TEXT NOT NULL
			)
		`,
	},
	{
		// As with api_keys: existing api_key plaintext rows become inert (the
		// column is dropped outright) — no migration path, by design.
		errContext: "migrate sync_remotes key columns",
		sql: `
			ALTER TABLE sync_remotes ADD COLUMN IF NOT EXISTS remote_api_key_id UUID;
			ALTER TABLE sync_remotes ADD COLUMN IF NOT EXISTS private_key_pem TEXT NOT NULL DEFAULT '';
			ALTER TABLE sync_remotes DROP COLUMN IF EXISTS api_key;
		`,
	},
	{
		errContext: "migrate maps sync_remote_id column",
		sql: `
			ALTER TABLE maps ADD COLUMN IF NOT EXISTS sync_remote_id UUID REFERENCES sync_remotes(id) ON DELETE SET NULL;
		`,
	},
	{
		// sync_all_maps defaults to true so every remote configured before
		// this feature existed keeps its prior full-mirror behavior
		// unchanged. sync_new_maps only matters when sync_all_maps is
		// false — it defaults to false (opt-in), matching the strict
		// reading of an explicit map selection: a newly appearing remote
		// map isn't synced until the admin either selects it or turns
		// this on.
		errContext: "migrate sync_remotes selective sync columns",
		sql: `
			ALTER TABLE sync_remotes ADD COLUMN IF NOT EXISTS sync_all_maps BOOLEAN NOT NULL DEFAULT true;
			ALTER TABLE sync_remotes ADD COLUMN IF NOT EXISTS sync_new_maps BOOLEAN NOT NULL DEFAULT false;
		`,
	},
	{
		// sync_geo_objects defaults to false (opt-in): unlike
		// sync_all_maps, there's no pre-existing behavior to preserve —
		// this is a wholly new capability, so existing remotes shouldn't
		// suddenly incur new pull volume without an admin turning it on.
		errContext: "migrate sync_remotes geo objects column",
		sql: `
			ALTER TABLE sync_remotes ADD COLUMN IF NOT EXISTS sync_geo_objects BOOLEAN NOT NULL DEFAULT false;
		`,
	},
	{
		// Holds the admin's explicit map selection for a remote whose
		// sync_all_maps is false (see internal/sync.mapsToSync). map_uuid
		// isn't a foreign key into maps(uuid): a selected map may not be
		// mirrored locally yet at the time it's selected.
		errContext: "migrate sync_remote_maps table",
		sql: `
			CREATE TABLE IF NOT EXISTS sync_remote_maps (
				remote_id  UUID NOT NULL REFERENCES sync_remotes(id) ON DELETE CASCADE,
				map_uuid   UUID NOT NULL,
				created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
				PRIMARY KEY (remote_id, map_uuid)
			)
		`,
	},
	{
		// scoped is an explicit on/off flag for api_key_scopes, deliberately
		// independent of that table's row count: an admin removing the last
		// individual map from a key's scope should leave it locked out of
		// everything, not silently revert it to unrestricted. Only
		// ClearAPIKeyScope resets it to false.
		errContext: "migrate api_keys scoped column",
		sql: `
			ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS scoped BOOLEAN NOT NULL DEFAULT false;
		`,
	},
	{
		// Per-key, per-map access grant restricting what a scoped API key
		// (see api_keys.scoped) may see or act on -- most notably, what a
		// server-sync remote's registered key may pull. versions NULL/empty
		// means every version of that map is in scope.
		errContext: "migrate api_key_scopes table",
		sql: `
			CREATE TABLE IF NOT EXISTS api_key_scopes (
				api_key_id UUID NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
				map_uuid   UUID NOT NULL REFERENCES maps(uuid) ON DELETE CASCADE,
				versions   TEXT[],
				granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
				PRIMARY KEY (api_key_id, map_uuid)
			);
			CREATE INDEX IF NOT EXISTS idx_api_key_scopes_api_key ON api_key_scopes (api_key_id);
		`,
	},
	{
		// Links a local account to the OpenID Connect identity it was
		// provisioned from (or later linked to), so a repeat login at the
		// same provider resolves back to the same account (see
		// FindUserByOIDCIdentity/CreateOIDCUser in oidc.go). Both columns
		// are '' for a password-only account. The unique index is partial
		// (WHERE oidc_subject <> '') so multiple password-only accounts
		// don't collide on the shared '' default.
		errContext: "migrate users oidc columns",
		sql: `
			ALTER TABLE users ADD COLUMN IF NOT EXISTS oidc_issuer  TEXT NOT NULL DEFAULT '';
			ALTER TABLE users ADD COLUMN IF NOT EXISTS oidc_subject TEXT NOT NULL DEFAULT '';
			CREATE UNIQUE INDEX IF NOT EXISTS idx_users_oidc_identity ON users (oidc_issuer, oidc_subject) WHERE oidc_subject <> '';
		`,
	},
	{
		// Records who did what to which resource, and when — every
		// mutating admin/API action across users, maps, permissions, geo
		// objects, api keys, and sync remotes (see
		// internal/handler.recordAudit and its call sites). entity_id is
		// TEXT rather than UUID since some entities have no single UUID
		// (e.g. a per-map permission grant is keyed by map uuid AND
		// username) and are recorded as a composite string instead.
		errContext: "migrate audit_logs table",
		sql: `
			CREATE TABLE IF NOT EXISTS audit_logs (
				id          BIGSERIAL PRIMARY KEY,
				occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
				actor       TEXT NOT NULL,
				action      TEXT NOT NULL,
				entity_type TEXT NOT NULL,
				entity_id   TEXT NOT NULL DEFAULT '',
				detail      TEXT NOT NULL DEFAULT ''
			);
			CREATE INDEX IF NOT EXISTS idx_audit_logs_occurred_at ON audit_logs (occurred_at DESC, id DESC);
			CREATE INDEX IF NOT EXISTS idx_audit_logs_entity ON audit_logs (entity_type, entity_id);
			CREATE INDEX IF NOT EXISTS idx_audit_logs_actor ON audit_logs (actor);
		`,
	},
	{
		// Links a local account to the LDAP directory entry it was
		// provisioned from (or later linked to), so a repeat login at the
		// same directory resolves back to the same account (see
		// FindUserByLDAPIdentity/CreateLDAPUser in ldap.go). '' for a
		// password-only or OIDC account. The unique index is partial
		// (WHERE ldap_dn <> ''), same reasoning as idx_users_oidc_identity
		// above.
		errContext: "migrate users ldap column",
		sql: `
			ALTER TABLE users ADD COLUMN IF NOT EXISTS ldap_dn TEXT NOT NULL DEFAULT '';
			CREATE UNIQUE INDEX IF NOT EXISTS idx_users_ldap_identity ON users (ldap_dn) WHERE ldap_dn <> '';
		`,
	},
	{
		// Removes the free-text display-name field: never used as an identity
		// key (LDAP/OIDC accounts link via ldap_dn/oidc_issuer+subject
		// instead), so dropping it loses no functionality.
		errContext: "migrate users drop cn column",
		sql: `
			ALTER TABLE users DROP COLUMN IF EXISTS cn;
		`,
	},
}

// Authenticate looks up username and verifies password against its bcrypt hash.
// It returns ErrInvalidCredentials for both an unknown username and a wrong password.
func (s *Store) Authenticate(ctx context.Context, username, password string) error {
	var hash string

	err := s.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE username = $1`, username).Scan(&hash)
	if err != nil {
		return ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return ErrInvalidCredentials
	}

	return nil
}

// Permissions holds a user's global create/edit/delete/admin capabilities.
// CanEditGeoObjects/CanDeleteGeoObjects are separate from CanEdit/CanDelete:
// they govern geo objects specifically and don't grant (or require) the
// ability to edit/delete the map itself, its versions, or its aliases.
type Permissions struct {
	CanCreate           bool
	CanEdit             bool
	CanDelete           bool
	CanEditGeoObjects   bool
	CanDeleteGeoObjects bool
	IsAdmin             bool
}

// GrantsMapVisibility reports whether p, on its own, is enough to make
// every map visible to its holder regardless of that map's own visibility
// settings or any per-map grant: being able to modify a map or its geo
// objects without being able to see it first would be nonsensical.
func (p Permissions) GrantsMapVisibility() bool {
	return p.IsAdmin || p.CanEdit || p.CanDelete || p.CanEditGeoObjects || p.CanDeleteGeoObjects
}

// GetPermissions returns the global permissions for username. Results are
// cached for cacheTTL, since this is looked up on every authenticated
// request (see requirePermission/canViewMap in internal/handler) but
// changes rarely.
func (s *Store) GetPermissions(ctx context.Context, username string) (Permissions, error) {
	if p, ok := s.permsCache.get(username); ok {
		return p, nil
	}

	var p Permissions

	err := s.pool.QueryRow(ctx, `
		SELECT can_create, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, is_admin FROM users WHERE username = $1
	`, username).Scan(&p.CanCreate, &p.CanEdit, &p.CanDelete, &p.CanEditGeoObjects, &p.CanDeleteGeoObjects, &p.IsAdmin)
	if err != nil {
		return Permissions{}, fmt.Errorf("get permissions for %q: %w", username, err)
	}

	s.permsCache.set(username, p)

	return p, nil
}

// SeedUser creates username with password if it doesn't already exist. Used to
// bootstrap the first account; it is a no-op if the username is already taken.
func (s *Store) SeedUser(ctx context.Context, username, password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO users (username, password_hash) VALUES ($1, $2)
		ON CONFLICT (username) DO NOTHING
	`, username, hash)
	if err != nil {
		return fmt.Errorf("seed user %q: %w", username, err)
	}

	return nil
}

// hashPassword bcrypt-hashes password for storage.
func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}

	return string(hash), nil
}

// isPgErrCode reports whether err is a *pgconn.PgError with the given SQLSTATE code.
func isPgErrCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}

// queryBuilder accumulates positional query arguments for a dynamically
// built SQL query, handing back the correct $N placeholder for each one.
// The SQL clause text surrounding that placeholder must always be a static
// Go string literal chosen by the caller — only argument values ever flow
// through bind — so a query built this way carries the same injection
// safety as one written with fixed placeholders throughout.
type queryBuilder struct {
	args []any
}

// bind appends value to the argument list and returns its $N placeholder.
func (q *queryBuilder) bind(value any) string {
	q.args = append(q.args, value)
	return fmt.Sprintf("$%d", len(q.args))
}

// collectRows runs query against pool and scans every returned row with
// scan, wrapping any error (including a scan failure) with label for
// context. It's shared by every Store List* method.
func collectRows[T any](ctx context.Context, pool *pgxpool.Pool, label, query string, scan func(pgx.Rows) (T, error), args ...any) ([]T, error) {
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	defer rows.Close()

	items := []T{}

	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", label, err)
		}

		items = append(items, v)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}

	return items, nil
}
