package filter

import (
	"github.com/RoaringBitmap/roaring/v2"
	"github.com/iOliverNguyen/loggi/internal/store"
)

// Plan is a query plan that combines a candidate bitmap (super-set of
// matching seqs, derived from indexed leaves) with a residual closure
// (applied to each candidate to enforce non-indexed predicates).
//
// Use with Store.QueryRangePlan. For live per-row evaluation, prefer Compile.
type Plan struct {
	// Candidates is the over-approximating set of seqs that MAY match.
	// nil means "all live seqs".
	Candidates *roaring.Bitmap
	// Residual is applied to each candidate. nil = always true.
	Residual store.EvalFn
}

// Eval returns a per-seq matcher that combines bitmap membership with
// residual evaluation.
func (p Plan) Eval() store.EvalFn {
	bm := p.Candidates
	res := p.Residual
	switch {
	case bm == nil && res == nil:
		return nil
	case bm == nil:
		return res
	case res == nil:
		return func(seq uint64) bool { return bm.Contains(uint32(seq)) }
	default:
		return func(seq uint64) bool {
			return bm.Contains(uint32(seq)) && res(seq)
		}
	}
}

// CompilePlan produces a query plan for n, using bitmap indexes where
// available. Pass nil for n to match everything.
func CompilePlan(n Node, s *store.Store) Plan {
	if n == nil {
		return Plan{}
	}
	return planNode(n, s)
}

func planNode(n Node, s *store.Store) Plan {
	switch x := n.(type) {
	case *AndNode:
		return planAnd(planNode(x.L, s), planNode(x.R, s))
	case *OrNode:
		return planOr(planNode(x.L, s), planNode(x.R, s))
	case *NotNode:
		// For NOT we can't reliably narrow candidates: even if the inner
		// plan has a bitmap, NOT inverts to "all live not in bitmap". We
		// drop bitmap candidates and inline the inverted predicate.
		inner := planNode(x.X, s).Eval()
		return Plan{Residual: func(seq uint64) bool {
			if inner == nil {
				return false
			}
			return !inner(seq)
		}}
	case *EqNode:
		// Hot Dict column → bitmap; otherwise residual closure.
		if len(x.Path) == 1 {
			if c := s.HotColumn(x.Path[0]); c != nil {
				if bm := c.BitmapForValue(x.V); bm != nil {
					return Plan{Candidates: bm}
				}
				// Hot column but value not in dict → empty result.
				if c.Kind == store.ColDict {
					return Plan{Candidates: roaring.New()}
				}
			}
		}
		return Plan{Residual: wrap(s, compile(x, s))}
	default:
		return Plan{Residual: wrap(s, compile(x, s))}
	}
}

func planAnd(l, r Plan) Plan {
	var cand *roaring.Bitmap
	switch {
	case l.Candidates != nil && r.Candidates != nil:
		cand = roaring.And(l.Candidates, r.Candidates)
	case l.Candidates != nil:
		cand = l.Candidates
	case r.Candidates != nil:
		cand = r.Candidates
	}
	res := combineAnd(l.Residual, r.Residual)
	return Plan{Candidates: cand, Residual: res}
}

func planOr(l, r Plan) Plan {
	// If both sides have bitmaps AND no residual, OR is a clean bitmap union.
	if l.Candidates != nil && r.Candidates != nil && l.Residual == nil && r.Residual == nil {
		return Plan{Candidates: roaring.Or(l.Candidates, r.Candidates)}
	}
	// Otherwise, fall back to a residual that asks each side. The candidate
	// bitmap (if any) is dropped because OR can match seqs outside the
	// individual bitmaps.
	le := l.Eval()
	re := r.Eval()
	return Plan{Residual: func(seq uint64) bool {
		if le != nil && le(seq) {
			return true
		}
		if re != nil && re(seq) {
			return true
		}
		return false
	}}
}

func combineAnd(a, b store.EvalFn) store.EvalFn {
	switch {
	case a == nil && b == nil:
		return nil
	case a == nil:
		return b
	case b == nil:
		return a
	default:
		return func(seq uint64) bool { return a(seq) && b(seq) }
	}
}
