package store

import (
	"encoding/json"
	"runtime"
	"testing"
)

// TestCompressionRepeatingTemplate validates the central design claim:
// ingesting many rows derived from a single repeating template uses far less
// memory than the wire-size sum of those rows would imply, and dictionary
// cardinality for highly-repeated fields stays small.
//
// Skipped under -short because it allocates ~1M rows.
func TestCompressionRepeatingTemplate(t *testing.T) {
	if testing.Short() {
		t.Skip("compression test allocates ~1M rows; skipped under -short")
	}

	const N = 1_000_000
	template := map[string]any{
		"level":      "info",
		"service":    "batch_worker",
		"env":        "local",
		"version":    "1.0.0",
		"caller":     "setup/dispatcher.go:232",
		"callerFunc": "apps/batch_worker/setup.(*Dispatcher).Start",
		"msg":        "dispatcher started",
		// A 30 KB-ish nested object that's identical across rows; should
		// be content-addressed in the blob slab and stored once.
		"app_conf": map[string]any{
			"start_timeout":    20000000000,
			"stop_timeout":     20000000000,
			"environment":      "local",
			"service":          "batch_worker",
			"version":          "unspecified",
			"pod_name":         "unknown",
			"giant_blob_field": longString(),
		},
	}
	raw, err := json.Marshal(template)
	if err != nil {
		t.Fatal(err)
	}

	// Make sure the row is large enough to demonstrate dedup wins.
	if len(raw) < 4000 {
		t.Fatalf("template is too small (%d bytes); test isn't meaningful", len(raw))
	}

	// Use a ring large enough to hold all rows so we measure compression,
	// not eviction.
	s := New(Options{Cap: 1 << 21}) // 2,097,152
	defer s.Close()

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	for i := 0; i < N; i++ {
		s.Append(AppendInput{JSON: raw})
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	heapBytes := after.HeapAlloc
	uncompressed := uint64(N) * uint64(len(raw))
	t.Logf("rows=%d row_size=%d uncompressed=%d MB heap=%d MB ratio=%.2fx",
		N, len(raw),
		uncompressed/(1<<20),
		heapBytes/(1<<20),
		float64(uncompressed)/float64(heapBytes),
	)

	// Hard ceiling: 1M rows × 4KB+ each = 4+ GB uncompressed; we should be
	// well under 1 GB heap because of dictionary + blob dedup.
	if heapBytes > 1<<30 {
		t.Fatalf("heap %d MB exceeds 1 GB ceiling for repeating template",
			heapBytes/(1<<20))
	}

	// Dictionary cardinality for highly-repeated fields must be tiny.
	if c := s.HotColumn("service"); c != nil {
		if card := c.Cardinality(); card > 4 {
			t.Errorf("service dict cardinality %d > 4 (template has 1 distinct value)", card)
		}
	}
	if c := s.HotColumn("level"); c != nil {
		if card := c.Cardinality(); card > 4 {
			t.Errorf("level dict cardinality %d > 4", card)
		}
	}

	// Blob slab should hold one giant config blob, not N copies.
	live, _, blobBytes := s.Blobs().Stats()
	if live > 8 {
		t.Errorf("blob slab live count %d > 8 (one giant blob expected)", live)
	}
	t.Logf("blob slab: live=%d bytes=%d", live, blobBytes)
}

// longString returns a deterministic ~5 KB string used inside the giant
// nested config blob to give content-addressed dedup something to work with.
func longString() string {
	const piece = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	out := make([]byte, 0, 5000)
	for len(out) < 5000 {
		out = append(out, piece...)
	}
	return string(out)
}
