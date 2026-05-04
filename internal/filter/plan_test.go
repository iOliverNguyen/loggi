package filter

import (
	"encoding/json"
	"testing"

	"github.com/iOliverNguyen/loggi/internal/store"
)

func setupPlan(t *testing.T) *store.Store {
	t.Helper()
	s := store.New(store.Options{Cap: 64})
	rows := []map[string]any{
		{"level": "info", "service": "a", "msg": "hello"},
		{"level": "warn", "service": "a", "msg": "slow"},
		{"level": "error", "service": "b", "msg": "boom"},
		{"level": "info", "service": "b", "msg": "ok"},
	}
	for _, r := range rows {
		raw, _ := json.Marshal(r)
		s.Publish(store.AppendInput{JSON: raw})
	}
	return s
}

// TestPlanBitmapEqUsesIndex: a single hot-column equality compiles to a
// non-nil candidate bitmap (no residual). Verifies the index path actually
// fires, not just functional equivalence.
func TestPlanBitmapEqUsesIndex(t *testing.T) {
	s := setupPlan(t)
	n, err := Parse("level:error")
	if err != nil {
		t.Fatal(err)
	}
	p := CompilePlan(n, s)
	if p.Candidates == nil {
		t.Fatal("expected candidate bitmap for hot Eq")
	}
	if p.Residual != nil {
		t.Fatal("expected nil residual for hot Eq")
	}
	if got := p.Candidates.GetCardinality(); got != 1 {
		t.Fatalf("want 1 match got %d", got)
	}
}

// TestPlanAndIntersection: two hot-Eq leaves under AND combine via bitmap
// intersection.
func TestPlanAndIntersection(t *testing.T) {
	s := setupPlan(t)
	n, _ := Parse("level:info service:b")
	p := CompilePlan(n, s)
	if p.Candidates == nil {
		t.Fatal("AND of two indexed Eqs should keep candidates")
	}
	if got := p.Candidates.GetCardinality(); got != 1 {
		t.Fatalf("want 1 got %d", got)
	}
}

// TestPlanResidualCleansUp: AND of indexed + non-indexed (msg substring)
// keeps the indexed bitmap as candidates and runs the substring as residual.
func TestPlanResidualCleansUp(t *testing.T) {
	s := setupPlan(t)
	n, _ := Parse("service:a *slow*")
	p := CompilePlan(n, s)
	if p.Candidates == nil {
		t.Fatal("indexed leaf should provide candidates")
	}
	if p.Residual == nil {
		t.Fatal("substring leaf should provide residual")
	}
	out := s.QueryRangeBitmap(p.Candidates, p.Residual, 0, 0, 100)
	if len(out) != 1 {
		t.Fatalf("want 1 match got %d", len(out))
	}
}
