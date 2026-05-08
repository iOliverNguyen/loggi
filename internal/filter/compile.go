package filter

import (
	"encoding/json"
	"strings"

	"github.com/iOliverNguyen/loggi/internal/store"
)

// LevelOrdinals maps log level names to comparison ordinals.
var LevelOrdinals = map[string]int{
	"trace": 0, "debug": 1, "info": 2, "warn": 3, "warning": 3, "error": 4, "fatal": 5,
}

// Compile turns an AST into an EvalFn closed over the given Store.
// nil node means "match all".
func Compile(n Node, s *store.Store) store.EvalFn {
	if n == nil {
		return nil
	}
	return compile(n, s)
}

func compile(n Node, s *store.Store) store.EvalFn {
	switch x := n.(type) {
	case *AndNode:
		l := compile(x.L, s)
		r := compile(x.R, s)
		return func(seq uint64) bool { return l(seq) && r(seq) }
	case *OrNode:
		l := compile(x.L, s)
		r := compile(x.R, s)
		return func(seq uint64) bool { return l(seq) || r(seq) }
	case *NotNode:
		inner := compile(x.X, s)
		return func(seq uint64) bool { return !inner(seq) }
	case *EqNode:
		return compileEq(x, s)
	case *SubstrNode:
		return compileSubstr(x, s)
	case *ExistsNode:
		return compileExists(x, s)
	case *RangeNode:
		return compileRange(x, s)
	case *CmpNumNode:
		return compileCmpNum(x, s)
	case *CmpStrNode:
		return compileCmpStr(x, s)
	}
	return func(uint64) bool { return false }
}

func compileEq(n *EqNode, s *store.Store) store.EvalFn {
	needle := n.V
	if len(n.Path) == 1 {
		name := n.Path[0]
		if s.HotColumn(name) != nil {
			return func(seq uint64) bool {
				v := s.HotString(seq, name)
				return v == needle || stringEqIgnoringQuotes(v, needle)
			}
		}
	}
	return func(seq uint64) bool {
		v := materializeField(s, seq, n.Path)
		return v == needle
	}
}

func compileSubstr(n *SubstrNode, s *store.Store) store.EvalFn {
	if n.Glob != nil {
		parts := n.Glob
		return func(seq uint64) bool {
			return matchGlob(materializeField(s, seq, n.Path), parts)
		}
	}
	needle := n.Needle
	if needle == "" {
		return func(uint64) bool { return true }
	}
	exact := n.Exact
	return func(seq uint64) bool {
		v := materializeField(s, seq, n.Path)
		if exact {
			return v == needle
		}
		return strings.Contains(v, needle)
	}
}

func compileExists(n *ExistsNode, s *store.Store) store.EvalFn {
	return func(seq uint64) bool {
		return materializeField(s, seq, n.Path) != ""
	}
}

// matchGlob walks an alternating literal/wild parts list against s.
// The pattern is anchored at both ends; wild parts greedily consume
// any-substring (including empty).
func matchGlob(s string, parts []GlobPart) bool {
	if len(parts) == 0 {
		return s == ""
	}
	// First literal must match at the start unless preceded by a wild.
	idx := 0
	i := 0
	if !parts[0].Wild {
		if !strings.HasPrefix(s, parts[0].Lit) {
			return false
		}
		i = len(parts[0].Lit)
		idx = 1
	}
	for idx < len(parts) {
		p := parts[idx]
		idx++
		if p.Wild {
			if idx >= len(parts) {
				// Trailing wild → match rest.
				return true
			}
			next := parts[idx]
			idx++
			if next.Wild {
				// Two consecutive wilds collapse — shouldn't happen but
				// be defensive.
				continue
			}
			pos := strings.Index(s[i:], next.Lit)
			if pos < 0 {
				return false
			}
			i += pos + len(next.Lit)
			continue
		}
		// Adjacent literal (only happens at start, handled above).
		if !strings.HasPrefix(s[i:], p.Lit) {
			return false
		}
		i += len(p.Lit)
	}
	// If the pattern didn't end with a wild, the entire input must
	// have been consumed.
	if !parts[len(parts)-1].Wild {
		return i == len(s)
	}
	return true
}

func compileRange(n *RangeNode, s *store.Store) store.EvalFn {
	return func(seq uint64) bool {
		f, ok := materializeNumber(s, seq, n.Path)
		if !ok {
			return false
		}
		return f >= n.Lo && f <= n.Hi
	}
}

func compileCmpNum(n *CmpNumNode, s *store.Store) store.EvalFn {
	return func(seq uint64) bool {
		f, ok := materializeNumber(s, seq, n.Path)
		if !ok {
			return false
		}
		switch n.Op {
		case ">":
			return f > n.N
		case ">=":
			return f >= n.N
		case "<":
			return f < n.N
		case "<=":
			return f <= n.N
		}
		return false
	}
}

func compileCmpStr(n *CmpStrNode, s *store.Store) store.EvalFn {
	op := n.Op
	// Special case: level ordinals.
	if len(n.Path) == 1 && n.Path[0] == "level" {
		want, ok := LevelOrdinals[strings.ToLower(n.V)]
		if !ok {
			return func(uint64) bool { return false }
		}
		return func(seq uint64) bool {
			v := s.HotString(seq, "level")
			got, ok := LevelOrdinals[strings.ToLower(v)]
			if !ok {
				return false
			}
			return cmpInt(got, want, op)
		}
	}
	want := n.V
	return func(seq uint64) bool {
		v := materializeField(s, seq, n.Path)
		return cmpStr(v, want, op)
	}
}

func cmpInt(a, b int, op string) bool {
	switch op {
	case ">":
		return a > b
	case ">=":
		return a >= b
	case "<":
		return a < b
	case "<=":
		return a <= b
	}
	return false
}
func cmpStr(a, b, op string) bool {
	switch op {
	case ">":
		return a > b
	case ">=":
		return a >= b
	case "<":
		return a < b
	case "<=":
		return a <= b
	}
	return false
}

func stringEqIgnoringQuotes(a, b string) bool {
	return strings.Trim(a, `"`) == strings.Trim(b, `"`)
}

// materializeField returns the string value of `path` for row at seq,
// crossing into tail-KV / nested JSON as needed. Returns "" if missing.
//
// Fast path: a single-segment path that names a hot column reads directly
// from the column without materializing the whole row.
func materializeField(s *store.Store, seq uint64, path []string) string {
	// `source` is a synthetic field resolved via the server-installed lookup.
	if len(path) == 1 && path[0] == "source" {
		return s.SourceName(s.SourceIDOfSeq(seq))
	}
	if len(path) == 1 {
		if s.HotColumn(path[0]) != nil {
			return s.HotString(seq, path[0])
		}
	}
	row := s.Materialize(seq)
	if row == nil {
		return ""
	}
	if len(path) == 1 {
		switch path[0] {
		case "level":
			return row.Level
		case "service":
			return row.Service
		case "msg":
			return row.Msg
		case "source":
			return s.SourceName(row.SourceID)
		}
	}
	// Walk into the Fields JSON object lazily.
	if len(row.Fields) == 0 {
		return ""
	}
	// Top level lookup
	var top map[string]json.RawMessage
	if err := json.Unmarshal(row.Fields, &top); err != nil {
		return ""
	}
	cur, ok := top[path[0]]
	if !ok {
		return ""
	}
	for i := 1; i < len(path); i++ {
		var inner map[string]json.RawMessage
		if err := json.Unmarshal(cur, &inner); err != nil {
			return ""
		}
		cur, ok = inner[path[i]]
		if !ok {
			return ""
		}
	}
	// String JSON values: strip quotes.
	if len(cur) > 0 && cur[0] == '"' {
		var s string
		if err := json.Unmarshal(cur, &s); err == nil {
			return s
		}
	}
	return strings.TrimSpace(string(cur))
}

func materializeNumber(s *store.Store, seq uint64, path []string) (float64, bool) {
	if len(path) == 1 {
		if c := s.HotColumn(path[0]); c != nil {
			if v, ok := s.HotF64(seq, path[0]); ok {
				return v, true
			}
		}
	}
	row := s.Materialize(seq)
	if row == nil {
		return 0, false
	}
	if len(row.Fields) == 0 {
		return 0, false
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(row.Fields, &top); err != nil {
		return 0, false
	}
	cur, ok := top[path[0]]
	if !ok {
		return 0, false
	}
	for i := 1; i < len(path); i++ {
		var inner map[string]json.RawMessage
		if err := json.Unmarshal(cur, &inner); err != nil {
			return 0, false
		}
		cur, ok = inner[path[i]]
		if !ok {
			return 0, false
		}
	}
	var f float64
	if err := json.Unmarshal(cur, &f); err != nil {
		return 0, false
	}
	return f, true
}
