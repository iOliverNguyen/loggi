package filter

import "testing"

// `!` is accepted as an alias for `-` (NOT prefix). Previously typing
// `!level:info` produced confusing errors — bare it surfaced as
// "unexpected trailing tokens", and once paren-wrapped (as
// computeEffectiveFilter does for pinned quick-chips) as
// "filter: missing ')'".
func TestBangNegation(t *testing.T) {
	s := setup(t)
	cases := []struct {
		expr string
		want int
	}{
		{"!level:info", 3},
		{"!level:info !service:mapper", 1},
		{"(!level:info) (!service:mapper)", 1}, // the reported bug
		{"!level:info -service:mapper", 1},     // mixed `!` and `-`
	}
	for _, c := range cases {
		got, err := count(s, c.expr)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.expr, err)
		}
		if got != c.want {
			t.Fatalf("count(%q) = %d, want %d", c.expr, got, c.want)
		}
	}
}

// `!ident` must produce the same AST as `-ident` — they go through the
// same NotNode wrapping. Verify by string-roundtrip.
func TestBangEqualsDash(t *testing.T) {
	for _, pair := range []struct{ bang, dash string }{
		{"!level:info", "-level:info"},
		{`!"quoted"`, `-"quoted"`},
		{"!(level:info OR level:warn)", "-(level:info OR level:warn)"},
		{"!@user.id:42", "-@user.id:42"},
	} {
		nb, err := Parse(pair.bang)
		if err != nil {
			t.Fatalf("Parse(%q): %v", pair.bang, err)
		}
		nd, err := Parse(pair.dash)
		if err != nil {
			t.Fatalf("Parse(%q): %v", pair.dash, err)
		}
		if nb.String() != nd.String() {
			t.Fatalf("%q -> %q; %q -> %q (want equal)", pair.bang, nb.String(), pair.dash, nd.String())
		}
	}
}
