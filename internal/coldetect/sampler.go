// Package coldetect samples the first N JSON log entries from a source and
// recommends a column set: which fields appear consistently enough, with
// useful enough cardinality, to deserve a dedicated column on the UI.
//
// The recommendation collapses cross-language synonyms via source.AliasMap
// (msg/message → msg, ts/timestamp → ts, caller/logger → caller) so a Go
// service and a Python service surface the same "message" column despite
// the wire-level key difference.
package coldetect

import (
	"encoding/json"
	"math"
	"sort"
	"time"

	"github.com/iOliverNguyen/loggi/internal/source"
)

const (
	// DefaultLimit caps how many JSON entries a single sampler will observe
	// before locking in its recommendation. ~150 is enough to see the
	// dominant shape of a stream without burning memory on a single
	// source's distinct-value sample sets.
	DefaultLimit = 150

	// DefaultTimeout is the wall-clock cap. Sources that emit fewer than
	// DefaultLimit entries within this window (e.g. a quiet container)
	// still get a recommendation computed from whatever was seen.
	DefaultTimeout = 30 * time.Second

	// distinctCap bounds the size of each field's distinct-value set, so
	// high-cardinality fields like trace_id don't grow unbounded during
	// sampling. Beyond the cap we stop adding new distinct values but keep
	// counting occurrences — distinct is then floor-clamped at cap which
	// is fine for the score's log(N/distinct) term.
	distinctCap = 64

	// maxValLen caps how many chars of a string value we measure; long
	// blobs only need to be flagged as "too long" once.
	maxValLen = 256

	// Scoring thresholds for non-priority fields. Priority fields (the
	// canonical ts/level/msg/service/caller — see source.Priorities) skip
	// these and only need source.PriorityPresence presence.
	minPresenceRatio = 0.6
	minDistinct      = 2
	maxValLen95      = 120 // generous: a SQL log line is usually fine, a stack-trace blob isn't
	maxDepth         = 2
	topK             = 9
)

// Sampler observes JSON entries from a single source and computes a
// recommended column set when sampling closes. Methods on a Sampler are
// NOT safe for concurrent use; callers should funnel observations through
// a single goroutine (the server's ingester satisfies this naturally).
type Sampler struct {
	limit    int
	deadline time.Time

	n      int // entries observed
	fields map[string]*fieldStat
}

type fieldStat struct {
	count    int
	distinct map[string]struct{}
	valLens  []int // sample of value char lengths, for p95
	depth    int   // max nesting depth seen for this field
	idLike   int   // distinct values matching looksLikeID
}

// New returns a Sampler that will accept up to limit entries or run until
// timeout elapses, whichever comes first. Zero values fall back to the
// package defaults.
func New(limit int, timeout time.Duration) *Sampler {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Sampler{
		limit:    limit,
		deadline: time.Now().Add(timeout),
		fields:   make(map[string]*fieldStat),
	}
}

// Observe decodes a JSON line and folds it into the sampler. Lines that
// fail to parse as a JSON object are silently ignored — the caller already
// filtered to JSON-mode rows. Returns true once the sampler has hit its
// cap and is ready to recommend.
func (s *Sampler) Observe(raw []byte) (done bool) {
	if s.n >= s.limit {
		return true
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false
	}
	s.n++
	for k, v := range obj {
		s.foldField(k, v, 1)
	}
	return s.n >= s.limit
}

// Deadline reports whether the sampler's wall-clock budget has expired.
// Callers should consult Deadline periodically (e.g. on each line or a
// ticker) — Observe alone won't fire it because a quiet source will never
// reach the limit.
func (s *Sampler) Deadline() bool { return time.Now().After(s.deadline) }

// N returns the number of entries observed so far.
func (s *Sampler) N() int { return s.n }

// foldField updates the stat record for one (key, value) pair. depth is
// the nesting level relative to the entry root (1 = top-level field).
// Nested objects increment depth and recurse using "parent.child" path
// keys; arrays are treated as leaves to avoid index churn.
func (s *Sampler) foldField(key string, raw json.RawMessage, depth int) {
	fs := s.fields[key]
	if fs == nil {
		fs = &fieldStat{distinct: make(map[string]struct{})}
		s.fields[key] = fs
	}
	fs.count++
	if depth > fs.depth {
		fs.depth = depth
	}

	val, isObj := unwrap(raw)
	// Recurse one level deeper than the column-promotion depth so we can
	// still discover aliased paths like loguru's `record.time.timestamp`
	// (depth 3). Non-aliased depth-3 leaves get filtered out at scoring
	// time; alias-resolved priorities skip the depth filter.
	if isObj && depth <= maxDepth+1 {
		var child map[string]json.RawMessage
		if err := json.Unmarshal(raw, &child); err == nil {
			for ck, cv := range child {
				s.foldField(key+"."+ck, cv, depth+1)
			}
			return
		}
	}
	if _, seen := fs.distinct[val]; !seen && len(fs.distinct) < distinctCap {
		fs.distinct[val] = struct{}{}
		if looksLikeID(val) {
			fs.idLike++
		}
	}
	if l := len(val); l > 0 {
		if l > maxValLen {
			l = maxValLen
		}
		fs.valLens = append(fs.valLens, l)
	}
}

// looksLikeID classifies a value as a synthetic identifier: long pure-digit
// or pure-hex strings (Datadog span/trace ids fit both shapes). Identifier
// fields are filtered out of non-priority recommendations — they make
// poor always-on columns (long, noisy) and the user typically wants them
// only when filtering. False positives on numeric metrics like
// "elapsed_ms" are avoided by the 12-char floor (a 12-digit ms count
// would be 11+ days — unrealistic for a single log entry).
func looksLikeID(v string) bool {
	if len(v) < 12 {
		return false
	}
	allDigit, allHex := true, true
	for i := 0; i < len(v); i++ {
		c := v[i]
		isDigit := c >= '0' && c <= '9'
		isHex := isDigit || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !isDigit {
			allDigit = false
		}
		if !isHex {
			allHex = false
		}
		if !allDigit && !allHex {
			return false
		}
	}
	if allDigit {
		return true
	}
	return allHex && len(v) >= 16
}

// unwrap returns (stringValue, isObject) for a JSON raw message. Strings
// are unquoted; other primitives keep their raw form (the byte sequence
// of the JSON literal, e.g. "3.14" or "true"). Object payloads return an
// empty value and isObject=true so the caller can recurse.
func unwrap(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	switch raw[0] {
	case '{':
		return "", true
	case '"':
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s, false
		}
		return string(raw), false
	default:
		return string(raw), false
	}
}

// Recommend returns the column ids the sampler suggests, in display
// order. Cross-language synonyms collapse onto one logical id (see
// source.AliasMap), so a Python `timestamp` and a Go `ts` both surface
// as a single "ts" column rather than two.
//
// The returned ids are either:
//   - A canonical logical id ("ts", "level", "msg", "service", "caller").
//     These are rendered by the alias-aware builtin renderers and always
//     lead the recommendation when present at ≥ source.PriorityPresence
//     presence — regardless of cardinality or value length.
//   - "@field.path" — a raw dotted JSON path for any non-priority field
//     that passes presence/distinct/length filters.
//
// Empty result when no entries were observed.
func (s *Sampler) Recommend() []string {
	if s.n == 0 {
		return nil
	}

	// First pass: pick the best raw key per logical id. A logical id like
	// "ts" can be satisfied by either "ts" or "timestamp" depending on the
	// source; we count the candidate's own presence (NOT a sum across
	// members) and keep the one with the highest presence so the picker
	// is deterministic even when both keys happen to appear.
	type cand struct {
		raw      string
		logical  string
		presence float64
		distinct int
		valLen95 int
		priority bool
	}
	N := float64(s.n)
	bestPerLogical := make(map[string]cand)
	var nonPriority []cand

	for raw, fs := range s.fields {
		presence := float64(fs.count) / N
		distinct := len(fs.distinct)
		vl95 := p95(fs.valLens)

		logical := source.Resolve(raw)
		isPriority := logical != "" && source.IsPriority(logical)
		// Depth filter applies only to non-priority fields. Priority
		// canonicals are allowed through at any depth so explicit aliases
		// like loguru's `record.time.timestamp` (depth 3) and tracing's
		// `span.service` surface as columns despite the nesting.
		if !isPriority && fs.depth > maxDepth {
			continue
		}
		if logical == "" {
			logical = "@" + raw
		}

		c := cand{
			raw:      raw,
			logical:  logical,
			presence: presence,
			distinct: distinct,
			valLen95: vl95,
			priority: isPriority,
		}
		if isPriority {
			if presence < source.PriorityPresence {
				continue
			}
			if prev, ok := bestPerLogical[logical]; !ok || presence > prev.presence {
				bestPerLogical[logical] = c
			}
			continue
		}
		if presence < minPresenceRatio || distinct < minDistinct || vl95 > maxValLen95 {
			continue
		}
		// Drop fields whose distinct values are mostly synthetic IDs —
		// span_ids, trace_ids, request_ids, etc. Useful as filter targets,
		// distracting as columns.
		if distinct > 0 && fs.idLike*2 >= distinct {
			continue
		}
		nonPriority = append(nonPriority, c)
	}

	// Priority canonicals lead, in the fixed source.Priorities order.
	out := make([]string, 0, topK)
	for _, p := range source.Priorities {
		if _, ok := bestPerLogical[p]; ok {
			out = append(out, p)
		}
	}

	// Non-priority remainder: rank by score, tie-break by raw key for
	// determinism. Score uses presence × log(N / distinct) — a TF-IDF-ish
	// signal that rewards common-but-not-unique fields (good facets).
	sort.SliceStable(nonPriority, func(i, j int) bool {
		si := nonPriority[i].presence * math.Log(N/float64(nonPriority[i].distinct))
		sj := nonPriority[j].presence * math.Log(N/float64(nonPriority[j].distinct))
		if si != sj {
			return si > sj
		}
		return nonPriority[i].raw < nonPriority[j].raw
	})
	for _, c := range nonPriority {
		if len(out) >= topK {
			break
		}
		out = append(out, c.logical)
	}
	return out
}

// p95 returns the 95th percentile of vals (or 0 if empty). Sorts a copy
// to avoid mutating caller state — len(vals) is at most one per observed
// entry per field, so allocation cost is negligible.
func p95(vals []int) int {
	if len(vals) == 0 {
		return 0
	}
	cp := make([]int, len(vals))
	copy(cp, vals)
	sort.Ints(cp)
	idx := int(math.Ceil(0.95*float64(len(cp)))) - 1
	idx = max(idx, 0)
	idx = min(idx, len(cp)-1)
	return cp[idx]
}
