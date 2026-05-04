package store

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// TestUnsubscribeNoCloseRace verifies that concurrent Unsubscribe + Publish
// does not panic by sending on a closed channel.
func TestUnsubscribeNoCloseRace(t *testing.T) {
	s := New(Options{Cap: 1024})

	const subs = 8
	ids := make([]uint64, subs)
	for i := range ids {
		sub := s.Subscribe(nil, 0)
		ids[i] = sub.ID
	}

	var wg sync.WaitGroup
	wg.Add(2)
	stop := make(chan struct{})
	go func() {
		defer wg.Done()
		raw, _ := json.Marshal(map[string]any{"msg": "x"})
		for {
			select {
			case <-stop:
				return
			default:
				s.Publish(AppendInput{JSON: raw})
			}
		}
	}()
	go func() {
		defer wg.Done()
		for _, id := range ids {
			s.Unsubscribe(id)
			time.Sleep(time.Microsecond)
		}
	}()

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestPauseResumeWithEvictionGap: pause, fill the ring past wrap, resume.
// The subscriber must observe a non-zero dropped count corresponding to the
// rows that were evicted while paused.
func TestPauseResumeWithEvictionGap(t *testing.T) {
	const cap = 8
	s := New(Options{Cap: cap})
	sub := s.Subscribe(nil, 0)
	defer s.Unsubscribe(sub.ID)

	// Pause immediately (cursor=head=0).
	s.Pause(sub.ID)

	raw, _ := json.Marshal(map[string]any{"msg": "x"})
	for i := 0; i < cap*3; i++ {
		s.Publish(AppendInput{JSON: raw})
	}
	// Cursor is still 0; tail is 2*cap. Resume should report that 2*cap rows
	// were dropped (gap).
	s.Resume(sub.ID)

	got := s.TakeDropped(sub.ID)
	if got < uint64(cap*2) {
		t.Fatalf("expected dropped >= %d, got %d", cap*2, got)
	}
}

// TestBackpressureCountsDropped: subscribe but never drain; Publish many rows.
// The dropped counter should grow once the channel fills.
func TestBackpressureCountsDropped(t *testing.T) {
	s := New(Options{Cap: 8192})
	sub := s.Subscribe(nil, 0)
	defer s.Unsubscribe(sub.ID)

	raw, _ := json.Marshal(map[string]any{"msg": "x"})
	const N = 4096
	for i := 0; i < N; i++ {
		s.Publish(AppendInput{JSON: raw})
	}
	// Channel cap is 1024; we sent 4096; ~3072 should be dropped.
	dropped := s.TakeDropped(sub.ID)
	if dropped < N-subChanCap {
		t.Fatalf("expected dropped >= %d, got %d", N-subChanCap, dropped)
	}
	if dropped > N {
		t.Fatalf("dropped overcounted: %d > %d", dropped, N)
	}
}

// TestUnsubscribeUnblocksPump verifies that Done channel signals correctly.
func TestUnsubscribeUnblocksPump(t *testing.T) {
	s := New(Options{Cap: 1024})
	sub := s.Subscribe(nil, 0)
	done := s.Done(sub.ID)
	if done == nil {
		t.Fatal("Done returned nil")
	}
	go s.Unsubscribe(sub.ID)
	select {
	case <-done:
		// ok
	case <-time.After(time.Second):
		t.Fatal("Done channel never signaled")
	}
}
