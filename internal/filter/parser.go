// Package filter implements a Datadog-style log filter DSL.
//
// Grammar (informal):
//   expr      := orExpr
//   orExpr    := andExpr ("OR" andExpr)*
//   andExpr   := notExpr (("AND" | <ws>) notExpr)*
//   notExpr   := ("-" | "NOT")? atom
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
			p.toks = append(p.toks, token{kind: tLBracket, text: "[", pos: i})
			i++
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
		case '>', '<':
			op := string(c)
			i++
			if i < len(s) && s[i] == '=' {
				op += "="
				i++
			}
			p.toks = append(p.toks, token{kind: tCmp, text: op, pos: i - len(op)})
			continue
		case '-':
			// Could be unary minus on a number, or NOT prefix. We look at
			// surroundings: if the prev token suggests a value position and
			// the next is a digit, treat as part of number; otherwise it's
			// a NOT prefix.
			isNeg := i+1 < len(s) && (s[i+1] >= '0' && s[i+1] <= '9')
			if isNeg && p.expectingValue() {
				j := i + 1
				for j < len(s) && (isDigit(s[j]) || s[j] == '.') {
					j++
				}
				num, _ := strconv.ParseFloat(s[i:j], 64)
				p.toks = append(p.toks, token{kind: tNumber, text: s[i:j], num: num, pos: i})
				i = j
				continue
			}
			p.toks = append(p.toks, token{kind: tDash, text: "-", pos: i})
			i++
			continue
		case '"':
			j := i + 1
			var b strings.Builder
			for j < len(s) && s[j] != '"' {
				if s[j] == '\\' && j+1 < len(s) {
					b.WriteByte(s[j+1])
					j += 2
					continue
				}
				b.WriteByte(s[j])
				j++
			}
			if j < len(s) {
				j++ // consume closing "
			}
			p.toks = append(p.toks, token{kind: tString, text: b.String(), pos: i})
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
	if len(p.toks) == 0 {
		return false
	}
	switch p.toks[len(p.toks)-1].kind {
	case tColon, tLBracket, tDotDot, tCmp:
		return true
	}
	return false
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }
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
