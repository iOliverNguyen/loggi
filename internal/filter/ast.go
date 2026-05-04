package filter

// Node is a parsed filter expression. Walk via type switch in the evaluator.
type Node interface {
	node()
	String() string
}

type AndNode struct{ L, R Node }
type OrNode struct{ L, R Node }
type NotNode struct{ X Node }

// EqNode matches Path equality with V (string equality).
type EqNode struct {
	Path []string
	V    string
}

// SubstrNode matches a substring within Path's string value.
// Exact = true means the value must equal V.
type SubstrNode struct {
	Path   []string
	Needle string
	Exact  bool
}

// RangeNode matches Path numeric value in [Lo, Hi].
type RangeNode struct {
	Path   []string
	Lo, Hi float64
}

// CmpNumNode matches Path numeric value against N with operator Op (one of
// ">", ">=", "<", "<=").
type CmpNumNode struct {
	Path []string
	Op   string
	N    float64
}

// CmpStrNode is the ordinal-string variant (e.g. level:>=info). The compiler
// resolves V to its level ordinal.
type CmpStrNode struct {
	Path []string
	Op   string
	V    string
}

func (*AndNode) node()     {}
func (*OrNode) node()      {}
func (*NotNode) node()     {}
func (*EqNode) node()      {}
func (*SubstrNode) node()  {}
func (*RangeNode) node()   {}
func (*CmpNumNode) node()  {}
func (*CmpStrNode) node()  {}

func (n *AndNode) String() string    { return "(" + n.L.String() + " AND " + n.R.String() + ")" }
func (n *OrNode) String() string     { return "(" + n.L.String() + " OR " + n.R.String() + ")" }
func (n *NotNode) String() string    { return "NOT " + n.X.String() }
func (n *EqNode) String() string     { return joinPath(n.Path) + ":" + n.V }
func (n *SubstrNode) String() string { return joinPath(n.Path) + ":~" + n.Needle }
func (n *RangeNode) String() string  { return joinPath(n.Path) + ":[range]" }
func (n *CmpNumNode) String() string { return joinPath(n.Path) + ":" + n.Op }
func (n *CmpStrNode) String() string { return joinPath(n.Path) + ":" + n.Op + n.V }

func joinPath(p []string) string {
	if len(p) == 0 {
		return ""
	}
	if len(p) == 1 {
		return p[0]
	}
	out := "@" + p[0]
	for i := 1; i < len(p); i++ {
		out += "." + p[i]
	}
	return out
}
