package filter

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/iOliverNguyen/loggi/internal/store"
)

// BenchmarkNestedFilterEval measures per-row eval cost for a 2-clause filter
// that touches the same nested object twice via materializeField/Number.
// Pre-refactor this re-parses row.Fields twice per row; post-refactor it should
// share one decode.
func BenchmarkNestedFilterEval(b *testing.B) {
	s := store.New(store.Options{Cap: 4096})
	for i := range 2048 {
		row := map[string]any{
			"level":   "info",
			"service": "api",
			"msg":     fmt.Sprintf("req %d done", i),
			"ts":      float64(i),
			"req": map[string]any{
				"status": 200 + (i % 5),
				"path":   fmt.Sprintf("/api/v1/items/%d", i),
				"method": "GET",
				"latency_ms": float64((i * 37) % 500),
			},
		}
		raw, _ := json.Marshal(row)
		s.Publish(store.AppendInput{JSON: raw})
	}

	// Both clauses pass for ~all rows so the AND doesn't short-circuit
	// and both materialize-via-Fields paths run per row.
	n, err := Parse(`@req.status:>=200 @req.path:*/items/*`)
	if err != nil {
		b.Fatalf("parse: %v", err)
	}
	fn := Compile(n, s)
	if fn == nil {
		b.Fatal("nil EvalFn")
	}

	lo, hi := s.Tail(), s.Head()
	b.ReportAllocs()
	for b.Loop() {
		matched := 0
		for seq := lo; seq < hi; seq++ {
			if fn(seq) {
				matched++
			}
		}
		// Defeat dead-code elimination.
		if matched < 0 {
			b.Fatal("impossible")
		}
	}
}
