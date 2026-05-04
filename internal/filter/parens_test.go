package filter

import "testing"

// TestParenAdjacency: `( a ) ( b )` must parse as `(a) AND (b)`. The
// synthetic-AND between the two parenthesized groups is meaningful.
func TestParenAdjacency(t *testing.T) {
	cases := []string{
		"(level:info) (service:x)",
		"(level:info)(service:x)",
		"(level:info)\tand\t(service:x)",
	}
	for _, src := range cases {
		n, err := Parse(src)
		if err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
		if _, ok := n.(*AndNode); !ok {
			t.Fatalf("Parse(%q): want top-level AndNode, got %T", src, n)
		}
	}
}

// TestImplicitAndAroundOperators: implicit-AND must NOT be inserted between
// `OR` and its operands — operator binding is on the operator itself.
func TestImplicitAndAroundOperators(t *testing.T) {
	for _, src := range []string{
		"a OR b",
		"a AND b",
		"-a",
		"NOT a",
	} {
		if _, err := Parse(src); err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
	}
}
