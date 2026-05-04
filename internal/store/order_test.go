package store

import (
	"encoding/json"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestPublishInOrderUnderConcurrency verifies that concurrent Publish calls
// produce a strictly monotonic sequence at the subscriber side. Without
// publishMu serialization the subscriber would see seqs interleaved
// out-of-order.
func TestPublishInOrderUnderConcurrency(t *testing.T) {
	const Goroutines = 8
	const PerGoroutine = 1000
	const Total = Goroutines * PerGoroutine

	s := New(Options{Cap: 1 << 14}) // 16384, plenty of room
	sub := s.Subscribe(nil, 0)
	defer s.Unsubscribe(sub.ID)

	// Drain into received[] in the order events arrive.
	out := s.Out(sub.ID)
	done := s.Done(sub.ID)
	received := make([]uint64, 0, Total)
	var rmu sync.Mutex
	stopRecv := make(chan struct{})
	doneRecv := make(chan struct{})
	go func() {
		defer close(doneRecv)
		for {
			select {
			case ev := <-out:
				rmu.Lock()
				received = append(received, ev.Seq)
				n := len(received)
				rmu.Unlock()
				if n >= Total {
					return
				}
			case <-done:
				return
			case <-stopRecv:
				return
			}
		}
	}()

	raw, _ := json.Marshal(map[string]any{"msg": "x"})

	var wg sync.WaitGroup
	wg.Add(Goroutines)
	for i := 0; i < Goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < PerGoroutine; j++ {
				s.Publish(AppendInput{JSON: raw})
			}
		}()
	}
	wg.Wait()

	// Wait for the receiver to drain everything (with a deadline).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rmu.Lock()
		n := len(received)
		rmu.Unlock()
		if n >= Total {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(stopRecv)
	<-doneRecv

	rmu.Lock()
	defer rmu.Unlock()
	if len(received) < Total {
		t.Fatalf("received %d/%d", len(received), Total)
	}

	// In-order assertion: seqs as received must be strictly monotonic.
	for i := 1; i < len(received); i++ {
		if received[i] <= received[i-1] {
			t.Fatalf("out-of-order at i=%d: prev=%d cur=%d", i, received[i-1], received[i])
		}
	}

	// Coverage assertion: after sorting we should see exactly [0..Total).
	sorted := append([]uint64(nil), received...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	for i, v := range sorted {
		if v != uint64(i) {
			t.Fatalf("missing seq at %d: got %d", i, v)
		}
	}
}

// TestAppendNoPartialRows: under concurrent ingest + reader probing, a
// reader must never see a row where seq < head but the row is unpopulated.
// Sentinel: every published row has level=info AND service=svc; readers
// asserting they see one but not the other indicate a torn read.
func TestAppendNoPartialRows(t *testing.T) {
	s := New(Options{Cap: 1 << 14})
	stop := atomic.Bool{}

	raw, _ := json.Marshal(map[string]any{
		"level": "info", "service": "svc", "msg": "x",
	})

	var wg sync.WaitGroup
	// Producer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			s.Publish(AppendInput{JSON: raw})
		}
	}()

	// Reader: probes Materialize on a recent seq.
	wg.Add(1)
	var torn atomic.Uint64
	go func() {
		defer wg.Done()
		for !stop.Load() {
			head := s.Head()
			if head == 0 {
				continue
			}
			seq := head - 1
			row := s.Materialize(seq)
			if row == nil {
				continue
			}
			if row.Level != "info" || row.Service != "svc" || row.Msg != "x" {
				torn.Add(1)
			}
		}
	}()

	time.Sleep(200 * time.Millisecond)
	stop.Store(true)
	wg.Wait()

	if n := torn.Load(); n > 0 {
		t.Fatalf("observed %d torn rows", n)
	}
}
