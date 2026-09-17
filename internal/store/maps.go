package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrMapNotFound is returned when a map lookup finds no matching row.
var ErrMapNotFound = errors.New("map not found")

// MapRecord is the persisted form of a map.
type MapRecord struct {
	UUID             uuid.UUID `json:"uuid" gorm:"column:uuid;type:uuid;primaryKey"`
	Name             string    `json:"name" gorm:"column:name;not null"`
	CurrentVersion   string    `json:"currentVersion" gorm:"column:current_version;not null;default:''"`
	VisibleToAll     bool      `json:"visibleToAll" gorm:"column:visible_to_all;not null;default:false"`
	AnonymousAllowed bool      `json:"anonymousAllowed" gorm:"column:anonymous_allowed;not null;default:false"`
	CreatedAt        time.Time `json:"createdAt" gorm:"column:created_at;not null;default:now()"`
	UpdatedAt        time.Time `json:"updatedAt" gorm:"column:updated_at;not null;default:now()"`
	CreatedBy        string    `json:"createdBy" gorm:"column:created_by;not null"`
	UpdatedBy        string    `json:"updatedBy" gorm:"column:updated_by;not null"`
	// OwnerID and SyncRemoteID back Owner/the sync_remote_id column; neither
	// is read/written through GORM's struct API directly (owner_id is
	// resolved to/from Owner below, sync_remote_id is only ever touched by
	// the raw SQL in sync_maps.go), but both must exist as real fields for
	// AutoMigrate to create the columns.
	OwnerID      int64      `json:"-" gorm:"column:owner_id;not null"`
	SyncRemoteID *uuid.UUID `json:"-" gorm:"column:sync_remote_id;type:uuid"`
	// Owner is the username of the user who can do everything with this
	// map (see isMapOwner in internal/webserver), regardless of global or
	// per-map grants. It starts out equal to CreatedBy but, unlike it, can
	// be transferred to another user later (see UpdateMapOwner).
	// Persisted as maps.owner_id, a foreign key to the owning user's
	// stable numeric id rather than a copy of their username text — the
	// `->` tag makes this a read-only, scan-only field: it's populated by
	// aliasing the joined owner's username as "owner" in every query that
	// reads a MapRecord (see mapOwnerJoin/loadMapWithOwner), and set
	// directly in Go (never via GORM) wherever a map's owner is already
	// known without a query (e.g. CreateMap).
	Owner string `json:"owner" gorm:"->;column:owner"`
}

// TableName implements the gorm.Tabler interface.
func (MapRecord) TableName() string { return "maps" }

// mapOwnerJoin joins maps to the users row its owner_id points at, aliased
// owner_user, and selects that owner's username as the synthetic "owner"
// column MapRecord.Owner scans from. Shared by every read that needs a full
// MapRecord including its owner.
func mapOwnerJoin(db *gorm.DB) *gorm.DB {
	return db.Joins("JOIN users owner_user ON owner_user.id = maps.owner_id").
		Select("maps.*, owner_user.username AS owner")
}

// loadMapWithOwner fetches map id via db (either s.db or a transaction),
// with Owner resolved. It returns ErrMapNotFound if id doesn't exist.
func loadMapWithOwner(ctx context.Context, db *gorm.DB, id uuid.UUID) (MapRecord, error) {
	var m MapRecord

	err := mapOwnerJoin(db.WithContext(ctx)).Where("maps.uuid = ?", id).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return MapRecord{}, ErrMapNotFound
	}

	if err != nil {
		return MapRecord{}, fmt.Errorf("load map: %w", err)
	}

	return m, nil
}

// lookupUserID resolves username to its numeric id. It returns
// ErrUserNotFound if username doesn't exist.
func lookupUserID(ctx context.Context, db *gorm.DB, username string) (int64, error) {
	var id int64

	err := db.WithContext(ctx).Model(&UserRecord{}).Select("id").Where("username = ?", username).Take(&id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, ErrUserNotFound
	}

	if err != nil {
		return 0, fmt.Errorf("look up user %q: %w", username, err)
	}

	return id, nil
}

// CreateMap inserts a new map row with a fresh UUID, created by and owned by
// createdBy.
func (s *Store) CreateMap(ctx context.Context, name, currentVersion string, visibleToAll, anonymousAllowed bool, createdBy string) (MapRecord, error) {
	ownerID, err := lookupUserID(ctx, s.db, createdBy)
	if err != nil {
		return MapRecord{}, fmt.Errorf("create map: %w", err)
	}

	m := MapRecord{
		UUID:             uuid.New(),
		Name:             name,
		CurrentVersion:   currentVersion,
		VisibleToAll:     visibleToAll,
		AnonymousAllowed: anonymousAllowed,
		CreatedBy:        createdBy,
		UpdatedBy:        createdBy,
		OwnerID:          ownerID,
		Owner:            createdBy,
	}

	if err := s.db.WithContext(ctx).Create(&m).Error; err != nil {
		return MapRecord{}, fmt.Errorf("create map: %w", err)
	}

	return m, nil
}

// MapFilter holds optional filters for ListMaps. A zero value matches every
// map the caller may otherwise see.
type MapFilter struct {
	Name             string // substring, case-insensitive
	CreatedBy        string // exact match
	VisibleToAll     *bool
	AnonymousAllowed *bool
}

// Scope applies f's set filters to db as additional WHERE conditions.
func (f MapFilter) Scope() func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if f.Name != "" {
			db = db.Where("name ILIKE ?", "%"+f.Name+"%")
		}

		if f.CreatedBy != "" {
			db = db.Where("created_by = ?", f.CreatedBy)
		}

		if f.VisibleToAll != nil {
			db = db.Where("visible_to_all = ?", *f.VisibleToAll)
		}

		if f.AnonymousAllowed != nil {
			db = db.Where("anonymous_allowed = ?", *f.AnonymousAllowed)
		}

		return db
	}
}

// ListMaps returns every map visible to username: maps marked visible to
// all, maps username owns, maps username holds a per-map view/edit/delete
// grant on (directly, or via any group username belongs to — see
// group_map_permissions/group_members, same sources Store.GetMapPermission
// unions), and (since they can already act on any map regardless of
// visibility) every map if bypassVisibility is true — meant to be passed as
// the acting user's is_admin || can_edit || can_delete. filter narrows the
// result further; its zero value matches everything.
func (s *Store) ListMaps(ctx context.Context, username string, bypassVisibility bool, filter MapFilter) ([]MapRecord, error) {
	maps := []MapRecord{}

	err := mapOwnerJoin(s.db.WithContext(ctx)).
		Where(`(? OR visible_to_all OR owner_user.username = ? OR EXISTS (
			SELECT 1 FROM map_permissions mp
			WHERE mp.map_uuid = maps.uuid AND mp.username = ?
			  AND (mp.can_view OR mp.can_edit OR mp.can_delete OR mp.can_edit_geo_objects OR mp.can_delete_geo_objects)
		) OR EXISTS (
			SELECT 1 FROM group_map_permissions gmp
			JOIN group_members gm ON gm.group_id = gmp.group_id
			WHERE gmp.map_uuid = maps.uuid AND gm.username = ?
			  AND (gmp.can_view OR gmp.can_edit OR gmp.can_delete OR gmp.can_edit_geo_objects OR gmp.can_delete_geo_objects)
		))`, bypassVisibility, username, username, username).
		Scopes(filter.Scope()).
		Order("maps.created_at DESC").
		Find(&maps).Error
	if err != nil {
		return nil, fmt.Errorf("list maps: %w", err)
	}

	return maps, nil
}

// GetMap fetches a single map by id. It returns ErrMapNotFound if it doesn't
// exist. Results are cached for cacheTTL: this is called on every tile
// request (to check visibility/anonymousAllowed) and every map-scoped
// route, but map rows change rarely.
func (s *Store) GetMap(ctx context.Context, id uuid.UUID) (MapRecord, error) {
	if m, ok := s.mapCache.get(id); ok {
		return m, nil
	}

	m, err := loadMapWithOwner(ctx, s.db, id)
	if err != nil {
		return MapRecord{}, err
	}

	s.mapCache.set(id, m)

	return m, nil
}

// GetCurrentVersion returns id's current version string. It's backed by a
// cache dedicated to this single field (separate from GetMap's MapRecord
// cache), since the version-file/bounds/geo-objects routes that resolve the
// "current" keyword only ever need this one value. It returns
// ErrMapNotFound if id doesn't exist.
func (s *Store) GetCurrentVersion(ctx context.Context, id uuid.UUID) (string, error) {
	if v, ok := s.currentVersionCache.get(id); ok {
		return v, nil
	}

	var version string

	err := s.db.WithContext(ctx).Model(&MapRecord{}).Select("current_version").Where("uuid = ?", id).Take(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrMapNotFound
	}

	if err != nil {
		return "", fmt.Errorf("get current version: %w", err)
	}

	s.currentVersionCache.set(id, version)

	return version, nil
}

// UpdateMap overwrites a map's name, currentVersion, and visibility flags.
// It returns ErrMapNotFound if id doesn't exist.
func (s *Store) UpdateMap(ctx context.Context, id uuid.UUID, name, currentVersion string, visibleToAll, anonymousAllowed bool, updatedBy string) (MapRecord, error) {
	res := s.db.WithContext(ctx).Model(&MapRecord{}).Where("uuid = ?", id).Updates(map[string]any{
		colName:             name,
		"current_version":   currentVersion,
		"visible_to_all":    visibleToAll,
		"anonymous_allowed": anonymousAllowed,
		colUpdatedBy:        updatedBy,
		colUpdatedAt:        gorm.Expr("now()"),
	})
	if res.Error != nil {
		return MapRecord{}, fmt.Errorf("update map: %w", res.Error)
	}

	if res.RowsAffected == 0 {
		return MapRecord{}, ErrMapNotFound
	}

	s.mapCache.invalidate(id)
	s.currentVersionCache.invalidate(id)

	return loadMapWithOwner(ctx, s.db, id)
}

// UpdateMapOwner transfers id's ownership to newOwner. It returns
// ErrMapNotFound if id doesn't exist, or ErrUserNotFound if newOwner isn't a
// real user.
func (s *Store) UpdateMapOwner(ctx context.Context, id uuid.UUID, newOwner string) (MapRecord, error) {
	ownerID, err := lookupUserID(ctx, s.db, newOwner)
	if err != nil {
		return MapRecord{}, err
	}

	res := s.db.WithContext(ctx).Model(&MapRecord{}).Where("uuid = ?", id).Update("owner_id", ownerID)
	if res.Error != nil {
		return MapRecord{}, fmt.Errorf("update map owner: %w", res.Error)
	}

	if res.RowsAffected == 0 {
		return MapRecord{}, ErrMapNotFound
	}

	s.mapCache.invalidate(id)

	return loadMapWithOwner(ctx, s.db, id)
}

// IncrementMapVersion reads the highest version recorded in map_versions for
// this map (treating no rows as 0), and atomically stores last+1 both as the
// map's current_version and as a new map_versions row. Using map_versions as
// the source of truth (rather than maps.current_version) means a PUT that
// manually edits currentVersion can't cause a later upload to reuse or
// overwrite an existing version directory.
//
// The map row lock held for the duration of the transaction serializes
// concurrent uploads for the same map so they can never be assigned the same
// version.
func (s *Store) IncrementMapVersion(ctx context.Context, id uuid.UUID, updatedBy string) (MapRecord, error) {
	var m MapRecord

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var exists bool

		err := tx.Raw(`SELECT true FROM maps WHERE uuid = ? FOR UPDATE`, id).Scan(&exists).Error
		if err != nil {
			return fmt.Errorf("lock map: %w", err)
		}

		if !exists {
			return ErrMapNotFound
		}

		var lastVersion int

		if err := tx.Raw(`SELECT COALESCE(MAX(version::int), 0) FROM map_versions WHERE map_uuid = ?`, id).Scan(&lastVersion).Error; err != nil {
			return fmt.Errorf("get last map version: %w", err)
		}

		nextVersion := strconv.Itoa(lastVersion + 1)

		res := tx.Model(&MapRecord{}).Where("uuid = ?", id).Updates(map[string]any{
			"current_version": nextVersion,
			colUpdatedBy:      updatedBy,
			colUpdatedAt:      gorm.Expr("now()"),
		})
		if res.Error != nil {
			return fmt.Errorf("update map version: %w", res.Error)
		}

		if err := tx.Create(&mapVersionModel{MapUUID: id, Version: nextVersion, CreatedBy: updatedBy}).Error; err != nil {
			return fmt.Errorf("record map version: %w", err)
		}

		m, err = loadMapWithOwner(ctx, tx, id)

		return err
	})
	if err != nil {
		return MapRecord{}, err
	}

	s.mapCache.invalidate(id)
	s.currentVersionCache.invalidate(id)

	return m, nil
}

// mapVersionModel is the GORM-mapped form of one row in map_versions.
// MapVersionRecord (the public type callers see) omits MapUUID since every
// caller already knows it from context; this unexported model carries the
// full column set AutoMigrate and IncrementMapVersion/RecordSyncedMapVersion
// need.
type mapVersionModel struct {
	MapUUID   uuid.UUID `gorm:"column:map_uuid;type:uuid;primaryKey"`
	Version   string    `gorm:"column:version;primaryKey"`
	CreatedAt time.Time `gorm:"column:created_at;not null;default:now()"`
	CreatedBy string    `gorm:"column:created_by;not null"`
}

func (mapVersionModel) TableName() string { return "map_versions" }

// MapVersionRecord is one uploaded version in a map's history.
type MapVersionRecord struct {
	Version   string    `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	CreatedBy string    `json:"createdBy"`
}

// ListMapVersions returns the upload history for a map, most recent first.
// It returns ErrMapNotFound if the map itself doesn't exist.
func (s *Store) ListMapVersions(ctx context.Context, id uuid.UUID) ([]MapVersionRecord, error) {
	if _, err := s.GetMap(ctx, id); err != nil {
		return nil, err
	}

	versions := []MapVersionRecord{}

	err := s.db.WithContext(ctx).Table("map_versions").
		Where("map_uuid = ?", id).
		Order("created_at DESC").
		Find(&versions).Error
	if err != nil {
		return nil, fmt.Errorf("list map versions: %w", err)
	}

	return versions, nil
}

// DeleteMap deletes a map by id (cascading to its versions and permission
// grants). It returns ErrMapNotFound if id doesn't exist.
func (s *Store) DeleteMap(ctx context.Context, id uuid.UUID) error {
	res := s.db.WithContext(ctx).Where("uuid = ?", id).Delete(&MapRecord{})
	if res.Error != nil {
		return fmt.Errorf("delete map: %w", res.Error)
	}

	if res.RowsAffected == 0 {
		return ErrMapNotFound
	}

	s.mapCache.invalidate(id)
	s.currentVersionCache.invalidate(id)

	return nil
}
