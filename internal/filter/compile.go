package filter

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/iOliverNguyen/loggi/internal/store"
)

// LevelOrdinals maps log level names to comparison ordinals.
var LevelOrdinals = map[string]int{
	"trace": 0, "debug": 1, "info": 2, "warn": 3, "warning": 3, "error": 4, "fatal": 5,
}

// evalCtx is a per-seq scratch pad shared across all compiled nodes in one
// eval pass. It memoises the Materialize call and the full Fields decode
// so a multi-clause filter that touches the same nested object twice pays
// for the parse once.
type evalCtx struct {
	s   *store.Store
	seq uint64

	row        *store.MaterializedRow
	rowFetched bool

	top        map[string]any
	topDecoded bool
}

func (c *evalCtx) Row() *store.MaterializedRow {
	if !c.rowFetched {
		c.row = c.s.Materialize(c.seq)
		c.rowFetched = true
	}
	return c.row
}

// Top returns the fully-decoded Fields tree. Nested objects are
// map[string]any, so path walks use cheap type assertions instead of
// re-Unmarshaling each level.
func (c *evalCtx) Top() map[string]any {
	if c.topDecoded {
		return c.top
	}
	c.topDecoded = true
	row := c.Row()
	if row == nil || len(row.Fields) == 0 {
		return nil
	}
	_ = json.Unmarshal(row.Fields, &c.top)
	return c.top
}

// evalNode is the internal closure shape: per-seq lookups go through a
// shared evalCtx. The public store.EvalFn wraps this at the Compile boundary.
type evalNode func(*evalCtx) bool

// Compile turns an AST into an EvalFn closed over the given Store.
// nil node means "match all".
func Compile(n Node, s *store.Store) store.EvalFn {
	if n == nil {
		return nil
	}
	return wrap(s, compile(n, s))
}

// wrap converts an evalNode into a store.EvalFn, creating one evalCtx per
// seq so a single eval pass through the node tree shares decoded state.
func wrap(s *store.Store, n evalNode) store.EvalFn {
	if n == nil {
		return nil
	}
	return func(seq uint64) bool {
		ctx := evalCtx{s: s, seq: seq}
		return n(&ctx)
	}
}

func compile(n Node, s *store.Store) evalNode {
	switch x := n.(type) {
	case *AndNode:
		l := compile(x.L, s)
		r := compile(x.R, s)
		return func(c *evalCtx) bool { return l(c) && r(c) }
	case *OrNode:
		l := compile(x.L, s)
		r := compile(x.R, s)
		return func(c *evalCtx) bool { return l(c) || r(c) }
	case *NotNode:
		inner := compile(x.X, s)
		return func(c *evalCtx) bool { return !inner(c) }
	case *EqNode:
		return compileEq(x, s)
	case *SubstrNode:
		return compileSubstr(x)
	case *ExistsNode:
		return compileExists(x)
	case *RangeNode:
		return compileRange(x)
	case *CmpNumNode:
		return compileCmpNum(x)
	case *CmpStrNode:
		return compileCmpStr(x)
	case *RegexNode:
		return compileRegex(x)
	}
	return func(*evalCtx) bool { return false }
}

func compileRegex(n *RegexNode) evalNode {
	re := n.Re
	if re == nil {
		return func(*evalCtx) bool { return false }
	}
	return func(c *evalCtx) bool {
		return re.MatchString(c.materializeField(n.Path))
	}
}

func compileEq(n *EqNode, s *store.Store) evalNode {
	needle := n.V
	if len(n.Path) == 1 {
		name := n.Path[0]
		if s.HotColumn(name) != nil {
			return func(c *evalCtx) bool {
				v := c.s.HotString(c.seq, name)
				return v == needle || stringEqIgnoringQuotes(v, needle)
			}
		}
	}
	return func(c *evalCtx) bool {
		v := c.materializeField(n.Path)
		return v == needle
	}
}

func compileSubstr(n *SubstrNode) evalNode {
	if n.Glob != nil {
		parts := n.Glob
		return func(c *evalCtx) bool {
			return matchGlob(c.materializeField(n.Path), parts)
		}
	}
	needle := n.Needle
	if needle == "" {
		return func(*evalCtx) bool { return true }
	}
	exact := n.Exact
	return func(c *evalCtx) bool {
		v := c.materializeField(n.Path)
		if exact {
			return v == needle
		}
		return strings.Contains(v, needle)
	}
}

func compileExists(n *ExistsNode) evalNode {
	return func(c *evalCtx) bool {
		return c.materializeField(n.Path) != ""
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

func compileRange(n *RangeNode) evalNode {
	return func(c *evalCtx) bool {
		f, ok := c.materializeNumber(n.Path)
		if !ok {
			return false
		}
		return f >= n.Lo && f <= n.Hi
	}
}

func compileCmpNum(n *CmpNumNode) evalNode {
	return func(c *evalCtx) bool {
		f, ok := c.materializeNumber(n.Path)
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

func compileCmpStr(n *CmpStrNode) evalNode {
	op := n.Op
	// Special case: level ordinals.
	if len(n.Path) == 1 && n.Path[0] == "level" {
		want, ok := LevelOrdinals[strings.ToLower(n.V)]
		if !ok {
			return func(*evalCtx) bool { return false }
		}
		return func(c *evalCtx) bool {
			v := c.s.HotString(c.seq, "level")
			got, ok := LevelOrdinals[strings.ToLower(v)]
			if !ok {
				return false
			}
			return cmpInt(got, want, op)
		}
	}
	want := n.V
	return func(c *evalCtx) bool {
		v := c.materializeField(n.Path)
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

// materializeField returns the string value of `path` for the ctx's seq,
// crossing into tail-KV / nested JSON as needed. Returns "" if missing.
//
// Fast path: a single-segment path that names a hot column reads directly
// from the column without materializing the whole row.
func (c *evalCtx) materializeField(path []string) string {
	s := c.s
	// `source` is a synthetic field resolved via the server-installed lookup.
	if len(path) == 1 && path[0] == "source" {
		return s.SourceName(s.SourceIDOfSeq(c.seq))
	}
	if len(path) == 1 {
		if s.HotColumn(path[0]) != nil {
			return s.HotString(c.seq, path[0])
		}
	}
	row := c.Row()
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
	top := c.Top()
	if top == nil {
		return ""
	}
	var cur any = top
	for _, seg := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur, ok = m[seg]
		if !ok {
			return ""
		}
	}
	return formatScalar(cur)
}

// formatScalar renders a decoded JSON scalar back to a comparable string.
// Strings come out unquoted, numbers in shortest round-trip form. Objects
// and arrays return "" — they aren't comparable to a string filter.
func formatScalar(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case nil:
		return ""
	}
	return ""
}

func (c *evalCtx) materializeNumber(path []string) (float64, bool) {
	s := c.s
	if len(path) == 1 {
		if hc := s.HotColumn(path[0]); hc != nil {
			if v, ok := s.HotF64(c.seq, path[0]); ok {
				return v, true
			}
		}
	}
	row := c.Row()
	if row == nil || len(row.Fields) == 0 {
		return 0, false
	}
	top := c.Top()
	if top == nil {
		return 0, false
	}
	var cur any = top
	for _, seg := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return 0, false
		}
		cur, ok = m[seg]
		if !ok {
			return 0, false
		}
	}
	switch x := cur.(type) {
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}
