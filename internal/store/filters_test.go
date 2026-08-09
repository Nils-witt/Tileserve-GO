package store

import (
	"reflect"
	"testing"
)

func TestMapFilterClauses(t *testing.T) {
	t.Run("zero value", func(t *testing.T) {
		qb := &queryBuilder{}
		if got := (MapFilter{}).clauses(qb); len(got) != 0 {
			t.Fatalf("clauses() = %v, want none", got)
		}
		if len(qb.args) != 0 {
			t.Fatalf("qb.args = %v, want none", qb.args)
		}
	})

	t.Run("all filters set", func(t *testing.T) {
		qb := &queryBuilder{}
		f := MapFilter{
			Name:             "park",
			CreatedBy:        "alice",
			VisibleToAll:     new(true),
			AnonymousAllowed: new(false),
		}
		got := f.clauses(qb)
		want := []string{
			"name ILIKE $1",
			"created_by = $2",
			"visible_to_all = $3",
			"anonymous_allowed = $4",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("clauses() = %v, want %v", got, want)
		}
		wantArgs := []any{"%park%", "alice", true, false}
		if !reflect.DeepEqual(qb.args, wantArgs) {
			t.Fatalf("qb.args = %v, want %v", qb.args, wantArgs)
		}
	})
}

func TestGeoObjectFilterClauses(t *testing.T) {
	t.Run("zero value", func(t *testing.T) {
		qb := &queryBuilder{}
		if got := (GeoObjectFilter{}).clauses(qb); len(got) != 0 {
			t.Fatalf("clauses() = %v, want none", got)
		}
	})

	t.Run("bbox set together", func(t *testing.T) {
		qb := &queryBuilder{}
		f := GeoObjectFilter{
			MinLat: new(1.0), MaxLat: new(2.0),
			MinLon: new(3.0), MaxLon: new(4.0),
		}
		got := f.clauses(qb)
		want := []string{"latitude BETWEEN $1 AND $2 AND longitude BETWEEN $3 AND $4"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("clauses() = %v, want %v", got, want)
		}
		wantArgs := []any{1.0, 2.0, 3.0, 4.0}
		if !reflect.DeepEqual(qb.args, wantArgs) {
			t.Fatalf("qb.args = %v, want %v", qb.args, wantArgs)
		}
	})

	t.Run("all scalar filters set", func(t *testing.T) {
		qb := &queryBuilder{}
		f := GeoObjectFilter{
			Name:       "hydrant",
			ExternalID: "ext-1",
			Street:     "main",
			Postcode:   "12345",
			CreatedBy:  "bob",
		}
		got := f.clauses(qb)
		want := []string{
			"name ILIKE $1",
			"external_id = $2",
			"street ILIKE $3",
			"postcode = $4",
			"created_by = $5",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("clauses() = %v, want %v", got, want)
		}
	})
}

func TestUserFilterClauses(t *testing.T) {
	t.Run("zero value", func(t *testing.T) {
		qb := &queryBuilder{}
		if got := (UserFilter{}).clauses(qb); len(got) != 0 {
			t.Fatalf("clauses() = %v, want none", got)
		}
	})

	t.Run("search reuses one bound placeholder twice", func(t *testing.T) {
		qb := &queryBuilder{}
		got := UserFilter{Search: "al"}.clauses(qb)
		want := []string{"(username ILIKE $1 OR cn ILIKE $1)"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("clauses() = %v, want %v", got, want)
		}
		wantArgs := []any{"%al%"}
		if !reflect.DeepEqual(qb.args, wantArgs) {
			t.Fatalf("qb.args = %v, want %v", qb.args, wantArgs)
		}
	})

	t.Run("all bool filters set", func(t *testing.T) {
		qb := &queryBuilder{}
		f := UserFilter{
			IsAdmin:   new(true),
			CanCreate: new(false),
			CanEdit:   new(true),
			CanDelete: new(false),
		}
		got := f.clauses(qb)
		want := []string{
			"is_admin = $1",
			"can_create = $2",
			"can_edit = $3",
			"can_delete = $4",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("clauses() = %v, want %v", got, want)
		}
	})
}

func TestQueryBuilderBind(t *testing.T) {
	qb := &queryBuilder{}
	if got := qb.bind("a"); got != "$1" {
		t.Errorf("bind() = %q, want $1", got)
	}
	if got := qb.bind("b"); got != "$2" {
		t.Errorf("bind() = %q, want $2", got)
	}
	wantArgs := []any{"a", "b"}
	if !reflect.DeepEqual(qb.args, wantArgs) {
		t.Fatalf("qb.args = %v, want %v", qb.args, wantArgs)
	}
}
