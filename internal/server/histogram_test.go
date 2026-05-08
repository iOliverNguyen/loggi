package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iOliverNguyen/loggi/internal/store"
)

// histogramResp mirrors the wire shape produced by handleAPIHistogram.
type histogramResp struct {
	BucketSeconds int     `json:"bucket_seconds"`
	From          float64 `json:"from"`
	To            float64 `json:"to"`
	Buckets       []struct {
		T     float64 `json:"t"`
		Error uint32  `json:"error"`
		Warn  uint32  `json:"warn"`
		Info  uint32  `json:"info"`
		Debug uint32  `json:"debug"`
		Other uint32  `json:"other"`
	} `json:"buckets"`
}

func newHistogramSrv(t *testing.T, rows []map[string]any) *Server {
	t.Helper()
	srv := NewServer(Options{StoreCap: 1024})
	for _, r := range rows {
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		srv.store.Publish(store.AppendInput{JSON: raw, SourceID: 1})
	}
	return srv
}

func callHistogram(t *testing.T, srv *Server, query string) histogramResp {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/histogram?"+query, nil)
	w := httptest.NewRecorder()
	srv.handleAPIHistogram(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200 got %d body=%s", w.Code, w.Body.String())
	}
	var resp histogramResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	return resp
}

func TestHistogramBuckets(t *testing.T) {
	// Three buckets of 100s each: [1000..1100), [1100..1200), [1200..1300).
	srv := newHistogramSrv(t, []map[string]any{
		{"ts": 1010.0, "level": "info", "msg": "a"},
		{"ts": 1050.0, "level": "warn", "msg": "b"},
		{"ts": 1099.999, "level": "error", "msg": "c"}, // last in bucket 0
		{"ts": 1100.0, "level": "info", "msg": "d"},    // bucket 1
		{"ts": 1250.0, "level": "debug", "msg": "e"},   // bucket 2
		{"ts": 1500.0, "level": "info", "msg": "f"},    // out of range
	})
	resp := callHistogram(t, srv, "bucket=100&from=1000&to=1300")

	if got := resp.BucketSeconds; got != 100 {
		t.Errorf("bucket_seconds: got %d want 100", got)
	}
	if got := len(resp.Buckets); got != 3 {
		t.Fatalf("len buckets: got %d want 3", got)
	}
	if b := resp.Buckets[0]; b.Info != 1 || b.Warn != 1 || b.Error != 1 {
		t.Errorf("bucket 0: %+v", b)
	}
	if b := resp.Buckets[1]; b.Info != 1 || b.Warn != 0 {
		t.Errorf("bucket 1: %+v", b)
	}
	if b := resp.Buckets[2]; b.Debug != 1 {
		t.Errorf("bucket 2: %+v", b)
	}
	// Bucket boundary: t for bucket 1 is from + 1*bucket = 1100.
	if got := resp.Buckets[1].T; got != 1100 {
		t.Errorf("bucket 1 t: got %v want 1100", got)
	}
}

func TestHistogramFilterPassthrough(t *testing.T) {
	srv := newHistogramSrv(t, []map[string]any{
		{"ts": 1010.0, "level": "info", "service": "auth"},
		{"ts": 1020.0, "level": "info", "service": "billing"},
		{"ts": 1030.0, "level": "error", "service": "auth"},
	})
	// Only `service:auth` should be tallied.
	resp := callHistogram(t, srv, "bucket=100&from=1000&to=1100&filter=service:auth")
	if got := len(resp.Buckets); got != 1 {
		t.Fatalf("len: %d", got)
	}
	b := resp.Buckets[0]
	if b.Info != 1 || b.Error != 1 {
		t.Errorf("auth-only counts: %+v", b)
	}
}

func TestHistogramEmptyWindow(t *testing.T) {
	srv := newHistogramSrv(t, []map[string]any{
		{"ts": 1010.0, "level": "info"},
	})
	resp := callHistogram(t, srv, "bucket=100&from=2000&to=2100")
	if len(resp.Buckets) != 1 {
		t.Fatalf("len: %d", len(resp.Buckets))
	}
	b := resp.Buckets[0]
	if b.Error+b.Warn+b.Info+b.Debug+b.Other != 0 {
		t.Errorf("expected zeros: %+v", b)
	}
}

func TestHistogramBadParams(t *testing.T) {
	srv := newHistogramSrv(t, nil)
	cases := []string{
		"bucket=0&from=1000&to=1100",
		"bucket=10&from=1100&to=1000",
		"bucket=10&from=1000&to=1000",
		fmt.Sprintf("bucket=1&from=0&to=%d", 5001), // > 5000 buckets
	}
	for _, q := range cases {
		r := httptest.NewRequest(http.MethodGet, "/api/histogram?"+q, nil)
		w := httptest.NewRecorder()
		srv.handleAPIHistogram(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("query %q: want 400 got %d body=%s", q, w.Code, w.Body.String())
		}
	}
}
