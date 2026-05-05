package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/iOliverNguyen/loggi/internal/filter"
)

// handleAPIExport streams matching rows directly to the response writer as
// either newline-delimited JSON ("jsonl", default) or a single JSON array
// ("json"). Query params:
//
//	format=jsonl|json
//	filter=<DSL>          optional; default = match all
//	limit=<n>             default 100000, max 1000000
//	from=<seq>            optional lo bound
//	to=<seq>              optional hi bound (exclusive)
func (s *Server) handleAPIExport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	format := q.Get("format")
	if format == "" {
		format = "jsonl"
	}
	if format != "jsonl" && format != "json" {
		http.Error(w, "format must be jsonl or json", http.StatusBadRequest)
		return
	}

	limit := 100_000
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 1_000_000 {
		limit = 1_000_000
	}

	var lo, hi uint64
	if v := q.Get("from"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			lo = n
		}
	}
	if v := q.Get("to"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			hi = n
		}
	}

	var fn func(uint64) bool
	if expr := q.Get("filter"); expr != "" {
		node, err := filter.Parse(expr)
		if err != nil {
			http.Error(w, "filter parse error: "+err.Error(), http.StatusBadRequest)
			return
		}
		fn = filter.Compile(node, s.store)
	}

	if format == "jsonl" {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Disposition", `attachment; filename="loggi-export.jsonl"`)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="loggi-export.json"`)
	}

	seqs := s.store.QueryRange(fn, lo, hi, limit)

	flusher, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)
	if format == "json" {
		_, _ = w.Write([]byte("[\n"))
	}
	// `written` counts rows actually emitted, so commas only ever appear
	// before an emitted row. Using the loop index would produce invalid
	// JSON (e.g. `[\n,\n{...}]`) when the leading rows have been evicted
	// and Materialize returns nil.
	written := 0
	for _, seq := range seqs {
		row := s.store.Materialize(seq)
		if row == nil {
			continue
		}
		if format == "json" && written > 0 {
			_, _ = w.Write([]byte(",\n"))
		}
		_ = enc.Encode(map[string]any{
			"seq":       row.Seq,
			"ts":        row.Ts,
			"source_id": row.SourceID,
			"source":    s.store.SourceName(row.SourceID),
			"level":     row.Level,
			"service":   row.Service,
			"msg":       row.Msg,
			"fields":    row.Fields,
			"text":      row.Text,
			"ansi":      row.Ansi,
		})
		written++
		// json.Encoder.Encode appends \n; for "json" format we just appended a
		// comma separately — that's fine, the trailing \n is harmless inside
		// an array.
		if written%512 == 0 && flusher != nil {
			flusher.Flush()
		}
	}
	if format == "json" {
		_, _ = w.Write([]byte("]\n"))
	}
}
