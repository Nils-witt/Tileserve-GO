package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// AuditLogEntry is one recorded administrative action: who (Actor) did what
// (Action) to which resource (EntityType/EntityID), and when.
type AuditLogEntry struct {
	ID         int64     `json:"id"`
	OccurredAt time.Time `json:"occurredAt"`
	Actor      string    `json:"actor"`
	Action     string    `json:"action"`
	EntityType string    `json:"entityType"`
	EntityID   string    `json:"entityId"`
	Detail     string    `json:"detail"`
}

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

// clauses returns the "column = $N"-style fragments for the filters set on
// f, binding their values through qb. Pure and DB-free so it's directly
// unit-testable.
func (f AuditLogFilter) clauses(qb *queryBuilder) []string {
	var clauses []string

	if f.Actor != "" {
		clauses = append(clauses, "actor = "+qb.bind(f.Actor))
	}

	if f.Action != "" {
		clauses = append(clauses, "action = "+qb.bind(f.Action))
	}

	if f.EntityType != "" {
		clauses = append(clauses, "entity_type = "+qb.bind(f.EntityType))
	}

	if f.EntityID != "" {
		clauses = append(clauses, "entity_id = "+qb.bind(f.EntityID))
	}

	if f.Since != nil {
		clauses = append(clauses, "occurred_at >= "+qb.bind(*f.Since))
	}

	if f.Until != nil {
		clauses = append(clauses, "occurred_at <= "+qb.bind(*f.Until))
	}

	return clauses
}

// RecordAuditLog appends one entry to the audit log. Callers treat this as
// best-effort observability (see internal/handler.recordAudit): a failure
// here is logged but never fails the mutating request it's describing.
func (s *Store) RecordAuditLog(ctx context.Context, actor, action, entityType, entityID, detail string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_logs (actor, action, entity_type, entity_id, detail)
		VALUES ($1, $2, $3, $4, $5)
	`, actor, action, entityType, entityID, detail)
	if err != nil {
		return fmt.Errorf("record audit log: %w", err)
	}

	return nil
}

// ListAuditLogs returns entries matching filter, most recent first. filter's
// zero value matches every entry. filter.Limit defaults to
// defaultAuditLogLimit and is capped at maxAuditLogLimit; filter.Offset below
// zero is treated as zero.
func (s *Store) ListAuditLogs(ctx context.Context, filter AuditLogFilter) ([]AuditLogEntry, error) {
	qb := &queryBuilder{}

	where := ""
	if clauses := filter.clauses(qb); len(clauses) > 0 {
		where = "WHERE " + strings.Join(clauses, " AND ")
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = defaultAuditLogLimit
	}

	limit = min(limit, maxAuditLogLimit)
	limitArg := qb.bind(limit)
	offsetArg := qb.bind(max(filter.Offset, 0))

	query := fmt.Sprintf(`
		SELECT id, occurred_at, actor, action, entity_type, entity_id, detail
		FROM audit_logs
		%s
		ORDER BY occurred_at DESC, id DESC
		LIMIT %s OFFSET %s
	`, where, limitArg, offsetArg)

	return collectRows(ctx, s.pool, "list audit logs", query, func(rows pgx.Rows) (AuditLogEntry, error) {
		var e AuditLogEntry

		err := rows.Scan(&e.ID, &e.OccurredAt, &e.Actor, &e.Action, &e.EntityType, &e.EntityID, &e.Detail)

		return e, err
	}, qb.args...)
}
