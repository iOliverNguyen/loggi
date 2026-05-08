package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/iOliverNguyen/loggi/internal/filter"
)

// handleAPIHistogram returns per-bucket counts of rows in [from, to)
// matching the optional filter, split by level. Used by the timeline
// strip to render volume bars and brush a time range.
//
// Query params:
//
//	bucket=<seconds>     bucket width; default 300; min 1, max 86400
//	from=<unix>          window lo (seconds); default now - bucket*120
//	to=<unix>            window hi (seconds, exclusive); default now
//	filter=<DSL>         optional; default = match all
//
// Response shape:
//
//	{
//	  "bucket_seconds": 300,
//	  "from": 1700000000,
//	  "to":   1700036000,
//	  "buckets": [
//	    { "t": 1700000000, "error": 0, "warn": 3, "info": 41, "debug": 12, "other": 0 },
//	    ...
//	  ]
//	}
//
// Buckets are emitted in chronological order, one per bucket-width slot;
// empty slots come back as zeroed entries so the client can render gaps.
func (s *Server) handleAPIHistogram(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	bucket := 300
	if v := q.Get("bucket"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			http.Error(w, "bucket must be a positive integer (seconds)", http.StatusBadRequest)
			return
		}
		if n > 86400 {
			n = 86400
		}
		bucket = n
	}

	now := float64(time.Now().UnixNano()) / 1e9
	to := now
	if v := q.Get("to"); v != "" {
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			http.Error(w, "to must be a unix timestamp", http.StatusBadRequest)
			return
		}
		to = n
	}
	from := to - float64(bucket)*120
	if v := q.Get("from"); v != "" {
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			http.Error(w, "from must be a unix timestamp", http.StatusBadRequest)
			return
		}
		from = n
	}
	if !(from < to) {
		http.Error(w, "from must be < to", http.StatusBadRequest)
		return
	}

	// Cap bucket count to keep the response bounded under abuse.
	span := to - from
	nBuckets := int((span + float64(bucket) - 1) / float64(bucket))
	if nBuckets > 5000 {
		http.Error(w, "too many buckets — widen `bucket` or narrow the range", http.StatusBadRequest)
		return
	}

	var fn func(uint64) bool
	if expr := q.Get("filter"); strings.TrimSpace(expr) != "" {
		node, err := filter.Parse(expr)
		if err != nil {
			http.Error(w, "filter parse error: "+err.Error(), http.StatusBadRequest)
			return
		}
		fn = filter.Compile(node, s.store)
	}

	type cell struct {
		Error uint32 `json:"error"`
		Warn  uint32 `json:"warn"`
		Info  uint32 `json:"info"`
		Debug uint32 `json:"debug"`
		Other uint32 `json:"other"`
	}
	cells := make([]cell, nBuckets)

	tail := s.store.Tail()
	head := s.store.Head()
	for seq := tail; seq < head; seq++ {
		ts, ok := s.store.HotF64(seq, "ts")
		if !ok {
			continue
		}
		if ts < from || ts >= to {
			continue
		}
		if fn != nil && !fn(seq) {
			continue
		}
		idx := int((ts - from) / float64(bucket))
		if idx < 0 || idx >= nBuckets {
			continue
		}
		switch strings.ToLower(s.store.HotString(seq, "level")) {
		case "error", "fatal", "panic":
			cells[idx].Error++
		case "warn", "warning":
			cells[idx].Warn++
		case "info", "notice":
			cells[idx].Info++
		case "debug", "trace":
			cells[idx].Debug++
		default:
			cells[idx].Other++
		}
	}

	type out struct {
		BucketSeconds int     `json:"bucket_seconds"`
		From          float64 `json:"from"`
		To            float64 `json:"to"`
		Buckets       []any   `json:"buckets"`
	}
	resp := out{
		BucketSeconds: bucket,
		From:          from,
		To:            to,
		Buckets:       make([]any, nBuckets),
	}
	for i := range cells {
		resp.Buckets[i] = struct {
			T     float64 `json:"t"`
			Error uint32  `json:"error"`
			Warn  uint32  `json:"warn"`
			Info  uint32  `json:"info"`
			Debug uint32  `json:"debug"`
			Other uint32  `json:"other"`
		}{
			T:     from + float64(i*bucket),
			Error: cells[i].Error,
			Warn:  cells[i].Warn,
			Info:  cells[i].Info,
			Debug: cells[i].Debug,
			Other: cells[i].Other,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
