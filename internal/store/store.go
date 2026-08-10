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
const cacheTTL = 15 * time.Second

type mapPermKey struct {
	mapID    uuid.UUID
	username string
}

// Store is the PostgreSQL-backed persistence layer for tileserve-go.
type Store struct {
	pool *pgxpool.Pool

	mapCache     *ttlCache[uuid.UUID, MapRecord]
	permsCache   *ttlCache[string, Permissions]
	mapPermCache *ttlCache[mapPermKey, MapPermission]
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
		pool:         pool,
		mapCache:     newTTLCache[uuid.UUID, MapRecord](cacheTTL),
		permsCache:   newTTLCache[string, Permissions](cacheTTL),
		mapPermCache: newTTLCache[mapPermKey, MapPermission](cacheTTL),
	}, nil
}

// Close releases the underlying database connection pool.
func (s *Store) Close() {
	s.pool.Close()
}

// Migrate creates the users, maps, map_versions, map_permissions, and
// geo_objects tables if they don't exist yet, and adds any columns
// introduced since the tables were first created. It is idempotent and safe
// to run on every startup.
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
			ALTER TABLE users ADD COLUMN IF NOT EXISTS cn         TEXT NOT NULL DEFAULT '';
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
type Permissions struct {
	CanCreate bool
	CanEdit   bool
	CanDelete bool
	IsAdmin   bool
}

// GetPermissions returns the global create/edit/delete/admin permissions for
// username. Results are cached for cacheTTL, since this is looked up on
// every authenticated request (see requirePermission/canViewMap in
// internal/handler) but changes rarely.
func (s *Store) GetPermissions(ctx context.Context, username string) (Permissions, error) {
	if p, ok := s.permsCache.get(username); ok {
		return p, nil
	}

	var p Permissions

	err := s.pool.QueryRow(ctx, `
		SELECT can_create, can_edit, can_delete, is_admin FROM users WHERE username = $1
	`, username).Scan(&p.CanCreate, &p.CanEdit, &p.CanDelete, &p.IsAdmin)
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
