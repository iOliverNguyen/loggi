// Package filter implements a Datadog-style log filter DSL.
//
// Grammar (informal):
//   expr      := orExpr
//   orExpr    := andExpr ("OR" andExpr)*
//   andExpr   := notExpr (("AND" | <ws>) notExpr)*
//   notExpr   := ("-" | "!" | "NOT")? atom
//   atom      := "(" expr ")" | term
//   term      := fieldTerm | bareTerm
//   fieldTerm := fieldRef ":" value
//   fieldRef  := IDENT | "@" IDENT ("." IDENT)*
//   value     := QSTRING | RANGE | CMPNUM | GLOB
//   RANGE     := "[" NUM ".." NUM "]"
//   CMPNUM    := (">=" | "<=" | ">" | "<") NUM
//   GLOB      := characters with optional '*' wildcards
//   bareTerm  := QSTRING | GLOB                  (msg substring)
//
// The parser is hand-rolled (small enough that participle would be overkill).
package filter

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Parse parses a filter expression. An empty input means "match all" (returns
// nil, nil).
func Parse(input string) (Node, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}
	p := &parser{src: input}
	p.tokenize()
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.toks) {
		return nil, fmt.Errorf("filter: unexpected trailing tokens at pos %d (%q)", p.toks[p.pos].pos, p.toks[p.pos].text)
	}
	return n, nil
}

type tokKind int

const (
	tIdent tokKind = iota
	tString
	tNumber
	tColon
	tDot
	tAt
	tLParen
	tRParen
	tLBracket
	tRBracket
	tDotDot
	tCmp // > >= < <=
	tStar
	tDash
	tAnd
	tOr
	tNot
)

type token struct {
	kind tokKind
	text string
	num  float64
	pos  int
	// parts is populated only for tString tokens whose source contained
	// at least one *unescaped* '*'. When set, the value is a glob
	// pattern (alternating literal/wild segments).
	parts []GlobPart
}

type parser struct {
	src  string
	toks []token
	pos  int
}

func (p *parser) tokenize() {
	s := p.src
	i := 0
	for i < len(s) {
		c := s[i]
		// Whitespace ends a glob token but otherwise is a separator (treated
		// as implicit AND by the parser at the andExpr level).
		if isSpace(c) {
			// Emit a synthetic AND so andExpr's loop knows there's a separator.
			p.toks = append(p.toks, token{kind: tAnd, text: " ", pos: i})
			for i < len(s) && isSpace(s[i]) {
				i++
			}
			continue
		}
		switch c {
		case '(':
			p.toks = append(p.toks, token{kind: tLParen, text: "(", pos: i})
			i++
			continue
		case ')':
			p.toks = append(p.toks, token{kind: tRParen, text: ")", pos: i})
			i++
			continue
		case ':':
			p.toks = append(p.toks, token{kind: tColon, text: ":", pos: i})
			i++
			continue
		case '@':
			p.toks = append(p.toks, token{kind: tAt, text: "@", pos: i})
			i++
			continue
		case '[':
			// `[NUM..NUM]` after a colon is a range. Otherwise the run is
			// a bare glob (e.g. `[ticket]` matches msg substring).
			if i+1 < len(s) && (isDigit(s[i+1]) || s[i+1] == '-') {
				p.toks = append(p.toks, token{kind: tLBracket, text: "[", pos: i})
				i++
				continue
			}
			j := consumeGlobRun(s, i)
			p.toks = append(p.toks, token{kind: tStar, text: s[i:j], pos: i})
			i = j
			continue
		case ']':
			p.toks = append(p.toks, token{kind: tRBracket, text: "]", pos: i})
			i++
			continue
		case '.':
			if i+1 < len(s) && s[i+1] == '.' {
				p.toks = append(p.toks, token{kind: tDotDot, text: "..", pos: i})
				i += 2
				continue
			}
			p.toks = append(p.toks, token{kind: tDot, text: ".", pos: i})
			i++
			continue
		case '/':
			// Regex literal `/pattern/flags` — only in value position so a
			// stray `/` elsewhere stays a glob char. Greedy: scan for the
			// closing `/` (skipping `\/`), then absorb trailing [ig]+ flags.
			// Falls through to glob-run if no closing slash is found, which
			// keeps a single bare `/` working as a substring char.
			if p.expectingValue() {
				if end, ok := scanRegexLiteral(s, i); ok {
					p.toks = append(p.toks, token{kind: tStar, text: s[i:end], pos: i})
					i = end
					continue
				}
			}
			j := consumeGlobRun(s, i)
			p.toks = append(p.toks, token{kind: tStar, text: s[i:j], pos: i})
			i = j
			continue
		case '>', '<':
			// tCmp only when in a value position (after `:`) AND followed
			// by `=` / digit / ident. Otherwise it's a bare glob run
			// (e.g. `-->`).
			if p.expectingValue() && i+1 < len(s) && (s[i+1] == '=' || isDigit(s[i+1]) || isIdentStart(s[i+1])) {
				op := string(c)
				i++
				if i < len(s) && s[i] == '=' {
					op += "="
					i++
				}
				p.toks = append(p.toks, token{kind: tCmp, text: op, pos: i - len(op)})
				continue
			}
			j := consumeGlobRun(s, i)
			p.toks = append(p.toks, token{kind: tStar, text: s[i:j], pos: i})
			i = j
			continue
		case '!':
			// `!` is an alias for `-` as a NOT prefix. Has no unary-minus
			// meaning, so the only branches are NOT-vs-glob-run.
			if i+1 < len(s) {
				c2 := s[i+1]
				if isIdentStart(c2) || c2 == '(' || c2 == '@' || c2 == '"' {
					p.toks = append(p.toks, token{kind: tDash, text: "!", pos: i})
					i++
					continue
				}
				j := consumeGlobRun(s, i)
				p.toks = append(p.toks, token{kind: tStar, text: s[i:j], pos: i})
				i = j
				continue
			}
			p.toks = append(p.toks, token{kind: tDash, text: "!", pos: i})
			i++
			continue
		case '-':
			// `-` has three possible meanings:
			//   1. Unary minus on a number, when in value position.
			//   2. NOT prefix, when followed by an opener (ident / `(` /
			//      `@` / `"` / digit-not-in-value-pos / `NOT`).
			//   3. Otherwise, part of a bare glob run (e.g. `-->`).
			if i+1 < len(s) {
				c2 := s[i+1]
				if isDigit(c2) && p.expectingValue() {
					j := i + 1
					for j < len(s) && (isDigit(s[j]) || s[j] == '.') {
						j++
					}
					num, _ := strconv.ParseFloat(s[i:j], 64)
					p.toks = append(p.toks, token{kind: tNumber, text: s[i:j], num: num, pos: i})
					i = j
					continue
				}
				if isIdentStart(c2) || c2 == '(' || c2 == '@' || c2 == '"' {
					p.toks = append(p.toks, token{kind: tDash, text: "-", pos: i})
					i++
					continue
				}
				// Glob run: '-' followed by something non-ident/non-opener.
				j := consumeGlobRun(s, i)
				p.toks = append(p.toks, token{kind: tStar, text: s[i:j], pos: i})
				i = j
				continue
			}
			p.toks = append(p.toks, token{kind: tDash, text: "-", pos: i})
			i++
			continue
		case '"':
			j := i + 1
			var b strings.Builder   // unescaped text (for EqNode etc.)
			var lit strings.Builder // current literal segment for parts
			var parts []GlobPart
			sawWild := false
			flushLit := func() {
				if lit.Len() > 0 {
					parts = append(parts, GlobPart{Lit: lit.String()})
					lit.Reset()
				}
			}
			for j < len(s) && s[j] != '"' {
				if s[j] == '\\' && j+1 < len(s) {
					b.WriteByte(s[j+1])
					lit.WriteByte(s[j+1])
					j += 2
					continue
				}
				if s[j] == '*' {
					b.WriteByte('*')
					sawWild = true
					flushLit()
					parts = append(parts, GlobPart{Wild: true})
					j++
					continue
				}
				b.WriteByte(s[j])
				lit.WriteByte(s[j])
				j++
			}
			if j < len(s) {
				j++ // consume closing "
			}
			tok := token{kind: tString, text: b.String(), pos: i}
			if sawWild {
				flushLit()
				tok.parts = parts
			}
			p.toks = append(p.toks, tok)
			i = j
			continue
		}

		// Numbers (don't eat '..' as part of the number)
		if isDigit(c) {
			j := i
			for j < len(s) {
				if isDigit(s[j]) {
					j++
					continue
				}
				if s[j] == '.' && j+1 < len(s) && s[j+1] != '.' && isDigit(s[j+1]) {
					j++
					continue
				}
				break
			}
			num, _ := strconv.ParseFloat(s[i:j], 64)
			p.toks = append(p.toks, token{kind: tNumber, text: s[i:j], num: num, pos: i})
			i = j
			continue
		}

		// Identifier / glob / keyword
		j := i
		for j < len(s) && !isSpace(s[j]) && !isDelim(s[j]) {
			j++
		}
		word := s[i:j]
		switch word {
		case "AND", "and":
			p.toks = append(p.toks, token{kind: tAnd, text: word, pos: i})
		case "OR", "or":
			p.toks = append(p.toks, token{kind: tOr, text: word, pos: i})
		case "NOT", "not":
			p.toks = append(p.toks, token{kind: tNot, text: word, pos: i})
		default:
			// Distinguish ident (alnum/underscore) from glob (contains '*' or non-ident).
			if isIdent(word) {
				p.toks = append(p.toks, token{kind: tIdent, text: word, pos: i})
			} else {
				p.toks = append(p.toks, token{kind: tStar, text: word, pos: i})
			}
		}
		i = j
	}
	// Drop spurious synthetic-AND tokens. A synthetic AND (text " ") is
	// meaningful only between a "closer" token (one that ends a complete
	// sub-expression: ident, string, number, glob, ')' or ']') and an
	// "opener" token (one that begins a new sub-expression: ident, string,
	// number, glob, '(', '@', '-', or NOT).
	out := p.toks[:0]
	for i, t := range p.toks {
		if t.kind == tAnd && t.text == " " {
			if len(out) == 0 || i+1 >= len(p.toks) {
				continue
			}
			prev := out[len(out)-1]
			next := p.toks[i+1]
			if !(isCloser(prev.kind) && isOpener(next.kind)) {
				continue
			}
		}
		out = append(out, t)
	}
	p.toks = out
}

func isCloser(k tokKind) bool {
	switch k {
	case tIdent, tString, tNumber, tStar, tRParen, tRBracket:
		return true
	}
	return false
}

func isOpener(k tokKind) bool {
	switch k {
	case tIdent, tString, tNumber, tStar, tLParen, tAt, tDash, tNot:
		return true
	}
	return false
}

func (p *parser) expectingValue() bool {
	// Walk back, skipping synthetic-AND (whitespace) tokens since the
	// caller hasn't filtered them yet.
	for i := len(p.toks) - 1; i >= 0; i-- {
		t := p.toks[i]
		if t.kind == tAnd && t.text == " " {
			continue
		}
		switch t.kind {
		case tColon, tLBracket, tDotDot, tCmp:
			return true
		}
		return false
	}
	return false
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isIdentStart(c byte) bool {
	return c == '_' ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z')
}

// consumeGlobRun returns the index past a bare glob token starting at i.
// Stops at whitespace, parens, or quote — which keeps boolean grouping
// and quoted strings as separate tokens. Used for `[ticket]`, `-->`,
// `>=>>`, and similar punctuation-ish runs the user might type bare.
func consumeGlobRun(s string, i int) int {
	j := i + 1
	for j < len(s) {
		c := s[j]
		if isSpace(c) || c == '(' || c == ')' || c == '"' {
			break
		}
		j++
	}
	return j
}
func isDelim(c byte) bool {
	switch c {
	case '(', ')', ':', '@', '[', ']', '"', '.', '>', '<':
		return true
	}
	return false
}
func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || unicode.IsLetter(r) {
			continue
		}
		if i > 0 && unicode.IsDigit(r) {
			continue
		}
		return false
	}
	return true
}

// --- Parser ---

func (p *parser) peek() (token, bool) {
	if p.pos >= len(p.toks) {
		return token{}, false
	}
	return p.toks[p.pos], true
}

func (p *parser) advance() token {
	t := p.toks[p.pos]
	p.pos++
	return t
}

func (p *parser) parseOr() (Node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		t, ok := p.peek()
		if !ok || t.kind != tOr {
			break
		}
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &OrNode{L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (Node, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for {
		t, ok := p.peek()
		if !ok {
			break
		}
		if t.kind == tAnd {
			p.advance()
			nxt, ok := p.peek()
			if !ok || nxt.kind == tOr || nxt.kind == tRParen {
				break
			}
		} else if isOpener(t.kind) {
			// Implicit AND with no whitespace between sub-expressions, e.g.
			// `(a)(b)` or `level:info -service:x`.
		} else {
			break
		}
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &AndNode{L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseNot() (Node, error) {
	t, ok := p.peek()
	if !ok {
		return nil, fmt.Errorf("filter: unexpected end of input")
	}
	if t.kind == tDash || t.kind == tNot {
		p.advance()
		inner, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		return &NotNode{X: inner}, nil
	}
	return p.parseAtom()
}

func (p *parser) parseAtom() (Node, error) {
	t, ok := p.peek()
	if !ok {
		return nil, fmt.Errorf("filter: unexpected end of input")
	}
	if t.kind == tLParen {
		p.advance()
		n, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		nxt, ok := p.peek()
		if !ok || nxt.kind != tRParen {
			return nil, fmt.Errorf("filter: missing ')'")
		}
		p.advance()
		return n, nil
	}
	return p.parseTerm()
}

func (p *parser) parseTerm() (Node, error) {
	t, ok := p.peek()
	if !ok {
		return nil, fmt.Errorf("filter: empty term")
	}
	if t.kind == tAt || t.kind == tIdent {
		// Look ahead: is the next non-path token a colon?
		save := p.pos
		path, _ := p.parseFieldRef()
		nxt, ok := p.peek()
		if ok && nxt.kind == tColon {
			p.advance()
			return p.parseFieldValue(path)
		}
		// Not a field term; treat ident as a bare msg substring.
		p.pos = save
		w := p.advance()
		return &SubstrNode{Path: []string{"msg"}, Needle: w.text}, nil
	}
	if t.kind == tString {
		p.advance()
		if t.parts != nil {
			return &SubstrNode{Path: []string{"msg"}, Glob: t.parts}, nil
		}
		return &SubstrNode{Path: []string{"msg"}, Needle: t.text, Exact: true}, nil
	}
	if t.kind == tStar {
		p.advance()
		needle := strings.Trim(t.text, "*")
		return &SubstrNode{Path: []string{"msg"}, Needle: needle}, nil
	}
	if t.kind == tNumber {
		p.advance()
		return &SubstrNode{Path: []string{"msg"}, Needle: t.text}, nil
	}
	return nil, fmt.Errorf("filter: unexpected token %q at pos %d", t.text, t.pos)
}

// parseFieldRef parses an identifier or '@a.b.c' chain. Returns the path
// segments (the leading '@' is consumed; the bare-ident form is path[0]).
func (p *parser) parseFieldRef() ([]string, error) {
	t, ok := p.peek()
	if !ok {
		return nil, fmt.Errorf("filter: expected field ref")
	}
	var path []string
	if t.kind == tAt {
		p.advance()
	}
	id, ok := p.peek()
	if !ok || id.kind != tIdent {
		return nil, fmt.Errorf("filter: expected identifier after '@'")
	}
	p.advance()
	path = append(path, id.text)
	for {
		nxt, ok := p.peek()
		if !ok || nxt.kind != tDot {
			break
		}
		// Is there an ident after the dot?
		if p.pos+1 >= len(p.toks) || p.toks[p.pos+1].kind != tIdent {
			break
		}
		p.advance() // dot
		seg := p.advance()
		path = append(path, seg.text)
	}
	return path, nil
}

func (p *parser) parseFieldValue(path []string) (Node, error) {
	t, ok := p.peek()
	if !ok {
		return nil, fmt.Errorf("filter: expected value after ':'")
	}
	switch t.kind {
	case tLBracket:
		p.advance()
		lo, err := p.expectNumber()
		if err != nil {
			return nil, err
		}
		dd, ok := p.peek()
		if !ok || dd.kind != tDotDot {
			return nil, fmt.Errorf("filter: expected '..' in range")
		}
		p.advance()
		hi, err := p.expectNumber()
		if err != nil {
			return nil, err
		}
		rb, ok := p.peek()
		if !ok || rb.kind != tRBracket {
			return nil, fmt.Errorf("filter: expected ']' after range")
		}
		p.advance()
		return &RangeNode{Path: path, Lo: lo, Hi: hi}, nil
	case tCmp:
		p.advance()
		// Comparison can be against a number OR an ordinal-encoded ident
		// (e.g. level:>=info).
		nxt, ok := p.peek()
		if !ok {
			return nil, fmt.Errorf("filter: expected value after %s", t.text)
		}
		if nxt.kind == tNumber {
			p.advance()
			return &CmpNumNode{Path: path, Op: t.text, N: nxt.num}, nil
		}
		if nxt.kind == tIdent || nxt.kind == tString {
			p.advance()
			return &CmpStrNode{Path: path, Op: t.text, V: nxt.text}, nil
		}
		return nil, fmt.Errorf("filter: bad value after %s", t.text)
	case tString, tIdent, tStar, tNumber:
		p.advance()
		// Regex literal: /pattern/flags. Detected on tStar tokens that
		// start with `/` and end with a `/` plus optional ig flags.
		if t.kind == tStar {
			if pat, flags, ok := splitRegexLiteral(t.text); ok {
				re, err := compileRegexLiteral(pat, flags)
				if err != nil {
					return nil, fmt.Errorf("filter: bad regex /%s/%s: %v", pat, flags, err)
				}
				return &RegexNode{Path: path, Pattern: pat, Flags: flags, Re: re}, nil
			}
		}
		// Quoted strings can carry a glob pattern (item 4): `key:"*foo*"`
		// → glob substring with `\*` literal-escape support.
		if t.kind == tString && t.parts != nil {
			return &SubstrNode{Path: path, Glob: t.parts}, nil
		}
		// Glob-style "contains" on any field: foo:*bar*, foo:bar*, foo:*bar.
		// The tokenizer emits any non-ident-shaped value as tStar, so the
		// presence of '*' in the literal text is the cue that the user
		// wanted a substring match rather than exact equality. A token of
		// only '*' chars (`field:*`, `field:**`) is the existence
		// predicate (item 3): match when the field is set & non-empty.
		if t.kind == tStar && strings.Contains(t.text, "*") {
			needle := strings.Trim(t.text, "*")
			if needle == "" {
				return &ExistsNode{Path: path}, nil
			}
			return &SubstrNode{Path: path, Needle: needle}, nil
		}
		return &EqNode{Path: path, V: t.text}, nil
	}
	return nil, fmt.Errorf("filter: unexpected value token %q", t.text)
}

func (p *parser) expectNumber() (float64, error) {
	t, ok := p.peek()
	if !ok || t.kind != tNumber {
		return 0, fmt.Errorf("filter: expected number")
	}
	p.advance()
	return t.num, nil
}

// scanRegexLiteral returns the end index past a `/pattern/flags` literal
// starting at `s[i]` (which must be `/`). Returns ok=false when no closing
// `/` is found before a space, so a stray bare `/` falls back to a glob.
func scanRegexLiteral(s string, i int) (int, bool) {
	if i >= len(s) || s[i] != '/' {
		return 0, false
	}
	j := i + 1
	for j < len(s) {
		c := s[j]
		if isSpace(c) {
			return 0, false
		}
		if c == '\\' && j+1 < len(s) {
			j += 2
			continue
		}
		if c == '/' {
			j++
			for j < len(s) && (s[j] == 'i' || s[j] == 'g') {
				j++
			}
			return j, true
		}
		j++
	}
	return 0, false
}

// splitRegexLiteral recognizes `/pattern/flags` where flags is "", "i", "g",
// or any combination of those. Returns (pattern, flags, true) on match. Only
// the trailing `/` followed by flag chars closes the literal; embedded `/`
// can be escaped with `\/` and is unescaped in the returned pattern.
func splitRegexLiteral(tok string) (pattern, flags string, ok bool) {
	if len(tok) < 2 || tok[0] != '/' {
		return "", "", false
	}
	// Find the trailing `/flags` suffix. Walk from the end.
	end := len(tok)
	for end > 1 {
		c := tok[end-1]
		if c == 'i' || c == 'g' {
			end--
			continue
		}
		break
	}
	if end <= 1 || tok[end-1] != '/' {
		return "", "", false
	}
	flags = tok[end:]
	body := tok[1 : end-1]
	if body == "" {
		return "", "", false
	}
	// Unescape `\/` → `/`.
	pattern = strings.ReplaceAll(body, `\/`, `/`)
	return pattern, flags, true
}

func compileRegexLiteral(pattern, flags string) (*regexp.Regexp, error) {
	// `g` is a JS-only "find all" flag; MatchString already scans the
	// whole value, so accepting `g` would silently behave identically
	// to no flag. Reject up front so the user knows their flag is being
	// ignored — and so the flag doesn't round-trip back as `g` and then
	// fail-parse on the next typed application.
	if strings.Contains(flags, "g") {
		return nil, fmt.Errorf("g flag not supported (regex already scans the whole value)")
	}
	pat := pattern
	if strings.Contains(flags, "i") {
		pat = "(?i)" + pat
	}
	return regexp.Compile(pat)
}
