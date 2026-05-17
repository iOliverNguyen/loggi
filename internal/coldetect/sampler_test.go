package coldetect

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestRecommend_Samples drives the sampler over the three reference shapes
// shipped under _docs/sample/. The recommendation is sorted before
// comparison because the sampler's tie-break ordering changes if scores
// happen to bunch; what matters for the UX is the SET of columns chosen,
// not the exact rank.
func TestRecommend_Samples(t *testing.T) {
	tests := []struct {
		name string
		file string
		// must contains ids that absolutely should be recommended.
		must []string
		// mustNot contains ids that absolutely should NOT be recommended
		// (typically high-cardinality identifiers and constants).
		mustNot []string
	}{
		{
			name:    "go",
			file:    "log-go.jsonl",
			must:    []string{"ts", "msg", "caller", "service", "level"},
			mustNot: []string{"@dd.trace_id", "@dd.span_id", "@trace_id", "@span_id", "@raw_span_id", "@raw_trace_id"},
		},
		{
			name:    "nodejs",
			file:    "log-nodejs.jsonl",
			must:    []string{"ts", "msg", "caller", "service", "level"},
			mustNot: []string{"@env", "@version"}, // constants — filtered for non-priority fields
		},
		{
			name: "python",
			file: "log-py.jsonl",
			must: []string{"ts", "msg", "caller", "service", "level"},
			// @lineno isn't asserted either way: with only 8 sample lines
			// it's indistinguishable from func_name by cardinality. In real
			// Python logs lineno's distinct/N ratio would be much higher
			// and the score would push it out organically.
			mustNot: []string{"@env", "@version"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(0, 0)
			feedFile(t, s, tt.file)
			got := s.Recommend()
			t.Logf("recommend: %v (n=%d)", got, s.N())

			gotSet := make(map[string]struct{}, len(got))
			for _, id := range got {
				gotSet[id] = struct{}{}
			}
			// `level` and `service` aren't in source.AliasMap — they appear
			// as @level/@service since the resolver returns "". The test
			// asserts the @-prefixed form to match what the sampler emits.
			for _, want := range tt.must {
				if _, ok := gotSet[want]; !ok {
					t.Errorf("missing recommended column %q (got %v)", want, sorted(got))
				}
			}
			for _, forbid := range tt.mustNot {
				if _, ok := gotSet[forbid]; ok {
					t.Errorf("unexpected recommended column %q (got %v)", forbid, sorted(got))
				}
			}
		})
	}
}

// TestRecommend_AliasCollapse verifies that mixing `ts` and `timestamp` in
// the same stream yields one canonical "ts" column rather than two.
func TestRecommend_AliasCollapse(t *testing.T) {
	s := New(0, 0)
	// 30 Go-shape, 30 Python-shape lines: same logical fields, different keys.
	for i := range 30 {
		s.Observe([]byte(`{"ts":1.5,"msg":"hi","caller":"go:1","level":"info","service":"a","seq":` +
			itoa(i) + `}`))
	}
	for i := range 30 {
		s.Observe([]byte(`{"timestamp":"2026-01-01T00:00:00Z","message":"hi","logger":"py.m","level":"info","service":"a","lineno":` +
			itoa(i) + `}`))
	}
	got := s.Recommend()
	counts := map[string]int{"ts": 0, "msg": 0, "caller": 0}
	for _, id := range got {
		if _, ok := counts[id]; ok {
			counts[id]++
		}
	}
	for k, c := range counts {
		if c != 1 {
			t.Errorf("expected exactly one %q in recommendation, got %d (full=%v)", k, c, got)
		}
	}
	// Neither raw side should leak through alongside the canonical id.
	for _, id := range got {
		if id == "@timestamp" || id == "@message" || id == "@logger" {
			t.Errorf("raw alias member %q leaked through alongside canonical id", id)
		}
	}
}

func feedFile(t *testing.T, s *Sampler, name string) {
	t.Helper()
	feedPath(t, s, filepath.Join("..", "..", "_docs", "sample", name))
}

func feedPath(t *testing.T, s *Sampler, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] != '{' {
			continue
		}
		s.Observe([]byte(line))
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
}

// TestRecommend_Cases drives the sampler over the 21 real-world fixtures
// under __/cases/{lang}{n}/output.jsonl. Per-fixture `want` lists only
// the canonicals the fixture's actual shape supports — a fixture with no
// caller-like field (e.g. nodejs1, ruby3 pino-style) genuinely can't
// surface one, and the test shouldn't pretend otherwise. ts/level/msg
// are universal across all fixtures.
func TestRecommend_Cases(t *testing.T) {
	universal := []string{"ts", "level", "msg"}
	cases := []struct {
		name  string
		extra []string // canonicals expected beyond ts/level/msg
	}{
		{"go1", []string{"service"}},
		{"go2", []string{"service", "caller"}},
		{"go3", []string{"service"}},
		{"java1", []string{"service", "caller"}},
		{"java2", []string{"service", "caller"}},
		{"java3", []string{"service", "caller"}},
		{"nodejs1", []string{"service"}},
		{"nodejs2", []string{"service"}},
		{"nodejs3", nil}, // pino: `name` doubles as service, intentionally not aliased
		{"php1", []string{"caller"}}, // channel is the caller-like signal; no service
		{"php2", []string{"caller"}},
		{"php3", []string{"service"}},
		{"py1", []string{"service", "caller"}},
		{"py2", []string{"service"}},
		{"py3", []string{"service", "caller"}}, // loguru: nested record.* via deep alias paths
		{"ruby1", []string{"service", "caller"}},
		{"ruby2", []string{"service"}}, // `application` aliases service; name=class, not aliased
		{"ruby3", nil}, // pino-ruby: same `name` overload as nodejs3
		{"rust1", []string{"service", "caller"}}, // span.service + target
		{"rust2", []string{"service"}},
		{"rust3", []string{"service", "caller"}}, // mdc.service + module_path
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := New(0, 0)
			feedPath(t, s, filepath.Join("..", "..", "__", "cases", c.name, "output.jsonl"))
			got := s.Recommend()
			gotSet := make(map[string]struct{}, len(got))
			for _, id := range got {
				gotSet[id] = struct{}{}
			}
			want := append(append([]string{}, universal...), c.extra...)
			var missing []string
			for _, id := range want {
				if _, ok := gotSet[id]; !ok {
					missing = append(missing, id)
				}
			}
			if len(missing) > 0 {
				t.Errorf("missing canonicals %v (got %v, n=%d)", missing, sorted(got), s.N())
			}
		})
	}
}

func sorted(xs []string) []string {
	cp := append([]string(nil), xs...)
	sort.Strings(cp)
	return cp
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
