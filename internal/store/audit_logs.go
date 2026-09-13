package store

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// AuditLogEntry is one recorded administrative action: who (Actor) did what
// (Action) to which resource (EntityType/EntityID), and when.
type AuditLogEntry struct {
	ID         int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	OccurredAt time.Time `json:"occurredAt" gorm:"column:occurred_at;not null;default:now()"`
	Actor      string    `json:"actor" gorm:"column:actor;not null"`
	Action     string    `json:"action" gorm:"column:action;not null"`
	EntityType string    `json:"entityType" gorm:"column:entity_type;not null"`
	EntityID   string    `json:"entityId" gorm:"column:entity_id;not null;default:''"`
	Detail     string    `json:"detail" gorm:"column:detail;not null;default:''"`
}

// TableName implements the gorm.Tabler interface.
func (AuditLogEntry) TableName() string { return "audit_logs" }

const (
	defaultAuditLogLimit = 100
	maxAuditLogLimit     = 500
)

// AuditLogFilter holds optional filters for ListAuditLogs. A zero value
// matches every entry, most recent first, capped at defaultAuditLogLimit.
type AuditLogFilter struct {
	Actor      string
	Action     string
	EntityType string
	EntityID   string
	Since      *time.Time
	Until      *time.Time
	Limit      int
	Offset     int
}

// Scope applies f's set filters to db as additional WHERE conditions.
func (f AuditLogFilter) Scope() func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if f.Actor != "" {
			db = db.Where("actor = ?", f.Actor)
		}

		if f.Action != "" {
			db = db.Where("action = ?", f.Action)
		}

		if f.EntityType != "" {
			db = db.Where("entity_type = ?", f.EntityType)
		}

		if f.EntityID != "" {
			db = db.Where("entity_id = ?", f.EntityID)
		}

		if f.Since != nil {
			db = db.Where("occurred_at >= ?", *f.Since)
		}

		if f.Until != nil {
			db = db.Where("occurred_at <= ?", *f.Until)
		}

		return db
	}
}

// RecordAuditLog appends one entry to the audit log. Callers treat this as
// best-effort observability (see internal/webserver/auditlog.RecordAudit): a failure
// here is logged but never fails the mutating request it's describing.
func (s *Store) RecordAuditLog(ctx context.Context, actor, action, entityType, entityID, detail string) error {
	e := AuditLogEntry{Actor: actor, Action: action, EntityType: entityType, EntityID: entityID, Detail: detail}

	if err := s.db.WithContext(ctx).Create(&e).Error; err != nil {
		return fmt.Errorf("record audit log: %w", err)
	}

	return nil
}

// ListAuditLogs returns entries matching filter, most recent first. filter's
// zero value matches every entry. filter.Limit defaults to
// defaultAuditLogLimit and is capped at maxAuditLogLimit; filter.Offset below
// zero is treated as zero.
func (s *Store) ListAuditLogs(ctx context.Context, filter AuditLogFilter) ([]AuditLogEntry, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultAuditLogLimit
	}

	limit = min(limit, maxAuditLogLimit)

	entries := []AuditLogEntry{}

	err := s.db.WithContext(ctx).Scopes(filter.Scope()).
		Order("occurred_at DESC, id DESC").
		Limit(limit).
		Offset(max(filter.Offset, 0)).
		Find(&entries).Error
	if err != nil {
		return nil, fmt.Errorf("list audit logs: %w", err)
	}

	return entries, nil
}
