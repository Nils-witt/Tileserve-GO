package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
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

// Column names shared by several Updates(map[string]any{...}) calls across
// this package.
const (
	colName      = "name"
	colUpdatedBy = "updated_by"
	colUpdatedAt = "updated_at"
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
	db                  *gorm.DB
	mapCache            *ttlCache[uuid.UUID, MapRecord]
	currentVersionCache *ttlCache[uuid.UUID, string]
	permsCache          *ttlCache[string, Permissions]
	mapPermCache        *ttlCache[mapPermKey, MapPermission]
	mapAliasCache       *ttlCache[mapAliasKey, string]
	apiKeyCache         *ttlCache[uuid.UUID, apiKeySigningKey]
	apiKeyScopeCache    *ttlCache[apiKeyScopeKey, apiKeyScopeEntry]
}

// NewStore opens a GORM connection to the postgres database at dsn and
// verifies it is reachable with a ping.
func NewStore(ctx context.Context, dsn string) (*Store, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get underlying sql.DB: %w", err)
	}

	// Explicit bounds rather than the library defaults: enough headroom for
	// concurrent request handling without letting a traffic spike open an
	// unbounded number of postgres connections. database/sql has no
	// equivalent of pgxpool's MinConns (proactively keeping connections
	// warm) — SetMaxIdleConns only caps idle connections from above.
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxLifetime(time.Hour)
	sqlDB.SetConnMaxIdleTime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return &Store{
		db:                  db,
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
	if sqlDB, err := s.db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

// Migrate creates every table this package uses (if it doesn't exist yet) in
// one step, via GORM's AutoMigrate, followed by a fixed batch of raw SQL for
// the schema pieces AutoMigrate's struct tags can't express: foreign keys
// (kept out of GORM's relationship-driven FK generation entirely, in favor
// of explicit, auditable ALTER TABLE statements — including the two
// composite foreign keys that reference map_versions' composite primary
// key, which GORM tags cannot express at all), and indexes that need a
// partial WHERE clause or explicit column ordering. It is idempotent and
// safe to run on every startup; unlike the incremental migration history
// this replaces, it always produces the current schema shape directly
// rather than replaying column-by-column history.
func (s *Store) Migrate(ctx context.Context) error {
	db := s.db.WithContext(ctx)

	if err := db.AutoMigrate(
		&UserRecord{},
		&MapRecord{},
		&mapVersionModel{},
		&GeoObjectRecord{},
		&mapPermissionModel{},
		&refreshTokenModel{},
		&mapVersionAliasModel{},
		&APIKeyRecord{},
		&SyncRemote{},
		&syncRemoteMapModel{},
		&apiKeyScopeModel{},
		&AuditLogEntry{},
		&GroupRecord{},
		&groupMemberModel{},
		&groupMapPermissionModel{},
	); err != nil {
		return fmt.Errorf("automigrate: %w", err)
	}

	if err := addConstraintsIfMissing(db, foreignKeys); err != nil {
		return err
	}

	for _, stmt := range secondaryIndexes {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}

	for _, stmt := range columnDefaults {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("set column default: %w", err)
		}
	}

	return nil
}

// namedConstraint is one foreign key to add if it doesn't already exist.
type namedConstraint struct {
	name string
	sql  string
}

// foreignKeys are every REFERENCES relationship in the schema, added as
// explicit, named constraints after AutoMigrate has created every table —
// deliberately not expressed via GORM association tags, so the exact
// on-delete behavior of each relationship (cascade, set null, or the
// default restrict) stays as plainly auditable as the hand-written SQL it
// replaces. Two of these (geo_objects and map_version_aliases) are
// composite foreign keys against map_versions' composite primary key, which
// GORM struct tags cannot express at all.
var foreignKeys = []namedConstraint{
	{"fk_maps_owner", `ALTER TABLE maps ADD CONSTRAINT fk_maps_owner FOREIGN KEY (owner_id) REFERENCES users(id)`},
	{"fk_maps_sync_remote", `ALTER TABLE maps ADD CONSTRAINT fk_maps_sync_remote FOREIGN KEY (sync_remote_id) REFERENCES sync_remotes(id) ON DELETE SET NULL`},
	{"fk_map_versions_map", `ALTER TABLE map_versions ADD CONSTRAINT fk_map_versions_map FOREIGN KEY (map_uuid) REFERENCES maps(uuid) ON DELETE CASCADE`},
	{"fk_geo_objects_map_version", `ALTER TABLE geo_objects ADD CONSTRAINT fk_geo_objects_map_version FOREIGN KEY (map_uuid, version) REFERENCES map_versions(map_uuid, version) ON DELETE CASCADE`},
	{"fk_map_permissions_map", `ALTER TABLE map_permissions ADD CONSTRAINT fk_map_permissions_map FOREIGN KEY (map_uuid) REFERENCES maps(uuid) ON DELETE CASCADE`},
	{"fk_map_permissions_user", `ALTER TABLE map_permissions ADD CONSTRAINT fk_map_permissions_user FOREIGN KEY (username) REFERENCES users(username) ON DELETE CASCADE`},
	{"fk_refresh_tokens_user", `ALTER TABLE refresh_tokens ADD CONSTRAINT fk_refresh_tokens_user FOREIGN KEY (username) REFERENCES users(username) ON DELETE CASCADE`},
	{"fk_map_version_aliases_map", `ALTER TABLE map_version_aliases ADD CONSTRAINT fk_map_version_aliases_map FOREIGN KEY (map_uuid) REFERENCES maps(uuid) ON DELETE CASCADE`},
	{"fk_map_version_aliases_map_version", `ALTER TABLE map_version_aliases ADD CONSTRAINT fk_map_version_aliases_map_version FOREIGN KEY (map_uuid, version) REFERENCES map_versions(map_uuid, version) ON DELETE CASCADE`},
	{"fk_api_keys_user", `ALTER TABLE api_keys ADD CONSTRAINT fk_api_keys_user FOREIGN KEY (username) REFERENCES users(username) ON DELETE CASCADE`},
	{"fk_sync_remote_maps_remote", `ALTER TABLE sync_remote_maps ADD CONSTRAINT fk_sync_remote_maps_remote FOREIGN KEY (remote_id) REFERENCES sync_remotes(id) ON DELETE CASCADE`},
	{"fk_api_key_scopes_key", `ALTER TABLE api_key_scopes ADD CONSTRAINT fk_api_key_scopes_key FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE CASCADE`},
	{"fk_api_key_scopes_map", `ALTER TABLE api_key_scopes ADD CONSTRAINT fk_api_key_scopes_map FOREIGN KEY (map_uuid) REFERENCES maps(uuid) ON DELETE CASCADE`},
	{"fk_group_members_group", `ALTER TABLE group_members ADD CONSTRAINT fk_group_members_group FOREIGN KEY (group_id) REFERENCES groups(id) ON DELETE CASCADE`},
	{"fk_group_members_user", `ALTER TABLE group_members ADD CONSTRAINT fk_group_members_user FOREIGN KEY (username) REFERENCES users(username) ON DELETE CASCADE`},
	{"fk_group_map_permissions_map", `ALTER TABLE group_map_permissions ADD CONSTRAINT fk_group_map_permissions_map FOREIGN KEY (map_uuid) REFERENCES maps(uuid) ON DELETE CASCADE`},
	{"fk_group_map_permissions_group", `ALTER TABLE group_map_permissions ADD CONSTRAINT fk_group_map_permissions_group FOREIGN KEY (group_id) REFERENCES groups(id) ON DELETE CASCADE`},
}

// addConstraintsIfMissing adds every constraint in constraints whose name
// isn't already present in pg_constraint. Postgres has no
// "ADD CONSTRAINT IF NOT EXISTS", unlike CREATE INDEX, so each one is
// existence-checked by name first to keep Migrate idempotent.
func addConstraintsIfMissing(db *gorm.DB, constraints []namedConstraint) error {
	for _, c := range constraints {
		var exists bool
		if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = ?)`, c.name).Scan(&exists).Error; err != nil {
			return fmt.Errorf("check constraint %s: %w", c.name, err)
		}

		if exists {
			continue
		}

		if err := db.Exec(c.sql).Error; err != nil {
			return fmt.Errorf("add constraint %s: %w", c.name, err)
		}
	}

	return nil
}

// secondaryIndexes are every index beyond a plain single-column unique
// constraint (already expressed via GORM's uniqueIndex tag) or a table's
// primary key: partial unique indexes (with a WHERE clause GORM's index tag
// syntax can't express), and plain multi-column indexes, kept here as raw
// SQL for the same auditability reason as foreignKeys above.
var secondaryIndexes = []string{
	`CREATE INDEX IF NOT EXISTS idx_geo_objects_map_version ON geo_objects (map_uuid, version)`,
	`CREATE INDEX IF NOT EXISTS idx_api_keys_username ON api_keys (username)`,
	`CREATE INDEX IF NOT EXISTS idx_api_key_scopes_api_key ON api_key_scopes (api_key_id)`,
	`CREATE INDEX IF NOT EXISTS idx_group_members_username ON group_members (username)`,
	`CREATE INDEX IF NOT EXISTS idx_audit_logs_occurred_at ON audit_logs (occurred_at DESC, id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_audit_logs_entity ON audit_logs (entity_type, entity_id)`,
	`CREATE INDEX IF NOT EXISTS idx_audit_logs_actor ON audit_logs (actor)`,
	// Partial unique indexes: '' is the "no linked identity" sentinel for a
	// password-only account/group (see the OIDCIssuer/OIDCSubject/LDAPDN and
	// LDAPGroupDN/OIDCGroupClaim fields), so the uniqueness constraint must
	// exclude it or every password-only row would collide on the shared ''
	// default.
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_oidc_identity ON users (oidc_issuer, oidc_subject) WHERE oidc_subject <> ''`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_ldap_identity ON users (ldap_dn) WHERE ldap_dn <> ''`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_groups_ldap_dn ON groups (ldap_group_dn) WHERE ldap_group_dn <> ''`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_groups_oidc_claim ON groups (oidc_group_claim) WHERE oidc_group_claim <> ''`,
}

// columnDefaults sets every DEFAULT clause this schema needs beyond what a
// column's Go zero value already gives it. These are deliberately not
// expressed via a GORM `default:` struct tag: GORM's create-time behavior
// silently replaces a Go zero value with a *literal* tagged default (as
// opposed to a function-shaped one like now()) whenever a field is left
// unset — which is exactly wrong for e.g. users.can_create, where an
// explicit false (an admin restricting a new user's permissions) is a real,
// meaningful value that must never be silently upgraded to the column's
// default of true. Setting these via a plain ALTER COLUMN instead keeps
// every application-level Create call (which always supplies every
// permission field explicitly, matching the hand-written SQL this replaces)
// unaffected, while still giving the column the same DEFAULT Postgres would
// apply for any insert that omits it outright (e.g. a manual psql insert).
var columnDefaults = []string{
	`ALTER TABLE users ALTER COLUMN can_create SET DEFAULT true`,
	`ALTER TABLE users ALTER COLUMN can_edit SET DEFAULT true`,
	`ALTER TABLE users ALTER COLUMN can_delete SET DEFAULT true`,
	`ALTER TABLE users ALTER COLUMN can_edit_geo_objects SET DEFAULT true`,
	`ALTER TABLE users ALTER COLUMN can_delete_geo_objects SET DEFAULT true`,
	`ALTER TABLE users ALTER COLUMN is_admin SET DEFAULT true`,
	`ALTER TABLE sync_remotes ALTER COLUMN enabled SET DEFAULT true`,
	`ALTER TABLE sync_remotes ALTER COLUMN sync_all_maps SET DEFAULT true`,
	`ALTER TABLE sync_remotes ALTER COLUMN poll_interval_sec SET DEFAULT 300`,
}

// Authenticate looks up username and verifies password against its bcrypt hash.
// It returns ErrInvalidCredentials for both an unknown username and a wrong password.
func (s *Store) Authenticate(ctx context.Context, username, password string) error {
	var hash string

	err := s.db.WithContext(ctx).Raw(`SELECT password_hash FROM users WHERE username = ?`, username).Row().Scan(&hash)
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
	CanCreate           bool `json:"canCreate"`
	CanEdit             bool `json:"canEdit"`
	CanDelete           bool `json:"canDelete"`
	CanEditGeoObjects   bool `json:"canEditGeoObjects"`
	CanDeleteGeoObjects bool `json:"canDeleteGeoObjects"`
	CanViewAll          bool `json:"canViewAll"`
	IsAdmin             bool `json:"isAdmin"`
}

// GrantsMapVisibility reports whether p, on its own, is enough to make
// every map visible to its holder regardless of that map's own visibility
// settings or any per-map grant: being able to modify a map or its geo
// objects without being able to see it first would be nonsensical, and
// CanViewAll exists precisely to grant that blanket visibility on its own.
func (p Permissions) GrantsMapVisibility() bool {
	return p.IsAdmin || p.CanEdit || p.CanDelete || p.CanEditGeoObjects || p.CanDeleteGeoObjects || p.CanViewAll
}

// GetPermissions returns the global permissions for username, OR'd together
// with the global permission bundle of every group username currently
// belongs to (see group_members/groups) — a group only ever adds
// capability, never removes it, same as a per-map grant. Results are cached
// for cacheTTL, since this is looked up on every authenticated request (see
// requirePermission/canViewMap in internal/webserver) but changes rarely. A
// nonexistent username still errors exactly as before: the UNION ALL
// produces zero total rows (group_members.username FKs to users, so a
// nonexistent user belongs to no groups either), and bool_or over zero rows
// is NULL, which fails the Scan below just like the previous plain-SELECT's
// ErrNoRows did.
func (s *Store) GetPermissions(ctx context.Context, username string) (Permissions, error) {
	if p, ok := s.permsCache.get(username); ok {
		return p, nil
	}

	var p Permissions

	err := s.db.WithContext(ctx).Raw(`
		SELECT
			bool_or(can_create), bool_or(can_edit), bool_or(can_delete),
			bool_or(can_edit_geo_objects), bool_or(can_delete_geo_objects),
			bool_or(can_view_all), bool_or(is_admin)
		FROM (
			SELECT can_create, can_edit, can_delete, can_edit_geo_objects, can_delete_geo_objects, can_view_all, is_admin
			FROM users WHERE username = ?
			UNION ALL
			SELECT g.can_create, g.can_edit, g.can_delete, g.can_edit_geo_objects, g.can_delete_geo_objects, g.can_view_all, g.is_admin
			FROM groups g
			JOIN group_members gm ON gm.group_id = g.id
			WHERE gm.username = ?
		) combined
	`, username, username).Row().Scan(&p.CanCreate, &p.CanEdit, &p.CanDelete, &p.CanEditGeoObjects, &p.CanDeleteGeoObjects, &p.CanViewAll, &p.IsAdmin)
	if err != nil {
		return Permissions{}, fmt.Errorf("get permissions for %q: %w", username, err)
	}

	s.permsCache.set(username, p)

	return p, nil
}

// SeedUser creates username with password if it doesn't already exist, with
// every global permission granted. Used to bootstrap the first account; it
// is a no-op if the username is already taken.
func (s *Store) SeedUser(ctx context.Context, username, password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}

	u := UserRecord{
		Username:            username,
		PasswordHash:        hash,
		CanCreate:           true,
		CanEdit:             true,
		CanDelete:           true,
		CanEditGeoObjects:   true,
		CanDeleteGeoObjects: true,
		IsAdmin:             true,
	}

	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&u).Error
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
