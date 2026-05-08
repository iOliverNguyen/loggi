package server

import (
	"encoding/json"
	"testing"

	"github.com/iOliverNguyen/loggi/internal/filter"
	"github.com/iOliverNguyen/loggi/internal/store"
)

// TestQueryBacklogSparseMatchesInOldSeqs is the regression test for the
// timeline-brush bug: when a filter's matches all live deeper in the ring
// than `limit` rows from head, queryBacklog must still return them.
//
// Before the fix queryBacklog truncated its scan window to [head-limit, head),
// so a brushed time-range whose matches sat below that window returned zero
// rows even though plenty of matches existed in the store.
func TestQueryBacklogSparseMatchesInOldSeqs(t *testing.T) {
	srv := NewServer(Options{StoreCap: 1024})
	const N = 500
	for i := range N {
		raw, _ := json.Marshal(map[string]any{
			"ts": 1778236500.0 + float64(i)*5.0, "level": "info", "msg": "x",
		})
		srv.store.Publish(store.AppendInput{JSON: raw})
	}
	if h := srv.store.Head(); h != N {
		t.Fatalf("head: got %d want %d", h, N)
	}

	sess := &session{srv: srv}
	// Filter only matches seqs 0..100 (ts 1778236500 .. 1778237000), well below
	// head-limit when limit=300.
	node, err := filter.Parse("ts:[1778236500..1778237000]")
	if err != nil {
		t.Fatal(err)
	}
	got := sess.queryBacklog(node, 300)
	if len(got) != 101 {
		t.Fatalf("got %d matches, want 101 (seqs 0..100)", len(got))
	}
	if got[0] != 0 || got[len(got)-1] != 100 {
		t.Fatalf("got seqs [%d..%d], want [0..100]", got[0], got[len(got)-1])
	}
}

// TestQueryBacklogReturnsLastLimitMatches verifies that when there are MORE
// matches than `limit`, queryBacklog returns the most recent `limit` of them
// (not the oldest, not a truncated scan window's worth).
func TestQueryBacklogReturnsLastLimitMatches(t *testing.T) {
	srv := NewServer(Options{StoreCap: 1024})
	const N = 500
	for i := range N {
		raw, _ := json.Marshal(map[string]any{
			"ts": 1778236500.0 + float64(i)*5.0, "level": "info", "msg": "x",
		})
		srv.store.Publish(store.AppendInput{JSON: raw})
	}

	sess := &session{srv: srv}
	// Match-everything filter; expect exactly the latest 300 seqs back.
	node, err := filter.Parse("level:info")
	if err != nil {
		t.Fatal(err)
	}
	got := sess.queryBacklog(node, 300)
	if len(got) != 300 {
		t.Fatalf("got %d, want 300", len(got))
	}
	if got[0] != 200 || got[len(got)-1] != 499 {
		t.Fatalf("got seqs [%d..%d], want [200..499]", got[0], got[len(got)-1])
	}
}
