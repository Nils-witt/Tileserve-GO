package store

import (
	"reflect"
	"testing"
	"time"
)

func TestAuditLogFilterClauses(t *testing.T) {
	t.Parallel()

	t.Run("zero value", func(t *testing.T) {
		t.Parallel()

		qb := &queryBuilder{}
		if got := (AuditLogFilter{}).clauses(qb); len(got) != 0 {
			t.Fatalf("clauses() = %v, want none", got)
		}

		if len(qb.args) != 0 {
			t.Fatalf("qb.args = %v, want none", qb.args)
		}
	})

	t.Run("all filters set", func(t *testing.T) {
		t.Parallel()

		since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		until := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

		qb := &queryBuilder{}
		f := AuditLogFilter{
			Actor:      "carol",
			Action:     "delete",
			EntityType: "map",
			EntityID:   "1234",
			Since:      &since,
			Until:      &until,
		}
		got := f.clauses(qb)

		want := []string{
			"actor = $1",
			"action = $2",
			"entity_type = $3",
			"entity_id = $4",
			"occurred_at >= $5",
			"occurred_at <= $6",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("clauses() = %v, want %v", got, want)
		}

		wantArgs := []any{"carol", "delete", "map", "1234", since, until}
		if !reflect.DeepEqual(qb.args, wantArgs) {
			t.Fatalf("qb.args = %v, want %v", qb.args, wantArgs)
		}
	})
}
