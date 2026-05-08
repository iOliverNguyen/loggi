package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/iOliverNguyen/loggi/internal/filter"
)

// handleAPIDebugFilter parses the supplied DSL expression, returns the
// AST as a string, and walks the most recent N live seqs in the store
// reporting each row's seq, ts (HotF64), level (HotString), and whether
// the compiled filter matches.
//
// Mounted only when Server was constructed with Options.Debug=true. Used
// by `./run server-debug` for diagnosing why a particular filter returns
// no rows.
//
// Query params:
//
//	expr=<DSL>   filter expression to evaluate; default "" (matches all)
//	n=<int>      how many rows to sample, newest first; default 20, max 1000
func (s *Server) handleAPIDebugFilter(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	expr := q.Get("expr")
	n := 20
	if v := q.Get("n"); v != "" {
		if k, err := strconv.Atoi(v); err == nil && k > 0 {
			n = k
		}
	}
	if n > 1000 {
		n = 1000
	}

	type rowOut struct {
		Seq     uint64  `json:"seq"`
		Ts      float64 `json:"ts"`
		TsOk    bool    `json:"ts_ok"`
		Level   string  `json:"level"`
		Matched bool    `json:"matched"`
	}
	type storeOut struct {
		Head uint64 `json:"head"`
		Tail uint64 `json:"tail"`
		Rows uint64 `json:"rows"`
	}
	type out struct {
		Expr       string   `json:"expr"`
		AST        string   `json:"ast"`
		ParseError string   `json:"parse_error,omitempty"`
		Store      storeOut `json:"store"`
		Rows       []rowOut `json:"rows"`
	}

	resp := out{Expr: expr}
	resp.Store.Head = s.store.Head()
	resp.Store.Tail = s.store.Tail()
	resp.Store.Rows = resp.Store.Head - resp.Store.Tail

	var match func(uint64) bool
	if strings.TrimSpace(expr) != "" {
		node, err := filter.Parse(expr)
		if err != nil {
			resp.ParseError = err.Error()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		resp.AST = node.String()
		match = filter.Compile(node, s.store)
	} else {
		match = func(uint64) bool { return true }
	}

	// Latest N seqs, newest-first.
	head := resp.Store.Head
	tail := resp.Store.Tail
	if head == tail {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}
	start := head
	stop := tail
	if uint64(n) < head-tail {
		stop = head - uint64(n)
	}
	for seq := start; seq > stop; seq-- {
		actual := seq - 1
		ts, ok := s.store.HotF64(actual, "ts")
		level := s.store.HotString(actual, "level")
		resp.Rows = append(resp.Rows, rowOut{
			Seq:     actual,
			Ts:      ts,
			TsOk:    ok,
			Level:   level,
			Matched: match(actual),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
