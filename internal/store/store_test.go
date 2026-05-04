package store

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestAppendAndMaterialize(t *testing.T) {
	s := New(Options{Cap: 16})
	raw, _ := json.Marshal(map[string]any{
		"level":   "info",
		"ts":      1777807314.275,
		"service": "batch_worker",
		"msg":     "dispatcher started",
		"trace":   "abc123",
	})
	seq := s.Publish(AppendInput{JSON: raw, SourceID: 1})
	if seq != 0 {
		t.Fatalf("first seq: want 0 got %d", seq)
	}
	row := s.Materialize(seq)
	if row == nil {
		t.Fatal("materialize: nil")
	}
	if row.Level != "info" || row.Service != "batch_worker" || row.Msg != "dispatcher started" {
		t.Fatalf("row mismatch: %+v", row)
	}
	if row.Ts < 1777807314 || row.Ts > 1777807315 {
		t.Fatalf("ts mismatch: %v", row.Ts)
	}
}

func TestRingEviction(t *testing.T) {
	const N = 8
	s := New(Options{Cap: N})
	// Append 3*N rows. The first 2*N must be evicted.
	for i := 0; i < 3*N; i++ {
		raw, _ := json.Marshal(map[string]any{
			"level":   "info",
			"service": fmt.Sprintf("svc-%d", i%4),
			"msg":     fmt.Sprintf("line %d", i),
		})
		s.Publish(AppendInput{JSON: raw, SourceID: 1})
	}
	if got := s.Tail(); got != uint64(2*N) {
		t.Fatalf("tail: want %d got %d", 2*N, got)
	}
	if got := s.Head(); got != uint64(3*N) {
		t.Fatalf("head: want %d got %d", 3*N, got)
	}
	// Old rows must be unmaterializable.
	if s.Materialize(0) != nil {
		t.Fatal("expected nil for evicted seq 0")
	}
	// Newest row must be present.
	if s.Materialize(uint64(3*N - 1)) == nil {
		t.Fatal("expected row for newest seq")
	}
}

func TestInternRefcountFreedAfterEviction(t *testing.T) {
	const N = 4
	s := New(Options{Cap: N})
	for i := 0; i < 3*N; i++ {
		raw, _ := json.Marshal(map[string]any{
			"level": "info",
			"msg":   fmt.Sprintf("unique-message-%d", i),
		})
		s.Publish(AppendInput{JSON: raw, SourceID: 0})
	}
	// After 3N appends with cap=N, all but the last N msg strings should be
	// freed (unique, no sharing). Live interner entries must be bounded.
	live, _, _ := s.intern.Stats()
	// Each row has level=info (shared) and a unique msg → expect ≈ N+1 live.
	if live > N+4 { // +slack for arena bookkeeping ids
		t.Fatalf("interner leaked: %d live (want ~%d)", live, N+1)
	}
}

func TestSubscribeLive(t *testing.T) {
	s := New(Options{Cap: 64})
	sub := s.Subscribe(nil, 0)
	defer s.Unsubscribe(sub.ID)

	go func() {
		raw, _ := json.Marshal(map[string]any{"msg": "x"})
		s.Publish(AppendInput{JSON: raw})
	}()

	ev, ok := <-s.Out(sub.ID)
	if !ok {
		t.Fatal("subscriber channel closed")
	}
	if ev.Seq != 0 {
		t.Fatalf("seq: want 0 got %d", ev.Seq)
	}
}
