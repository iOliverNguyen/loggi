package server

import (
	"math"
	"testing"
	"time"

	"github.com/iOliverNguyen/loggi/internal/source"
)

// TestTextLineParsedTimestampAndLevel confirms processLine extracts both
// a level and a timestamp from non-JSON lines and writes them to the
// store row. The expected values mirror what source.ParseTextLine
// produces so this test catches the wiring (server.go) rather than the
// parsing logic (covered by source.TestParseTextLine_*).
func TestTextLineParsedTimestampAndLevel(t *testing.T) {
	srv := NewServer(Options{
		HTTPBind:    "127.0.0.1:0",
		IdleTimeout: time.Hour,
		StoreCap:    1024,
	})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()

	lines := [][]byte{
		[]byte(`2026-05-26T04:16:50.051Z [info] [AMQP]: Created AMQP queue: amq.gen-foo`),
		[]byte(`2026-05-26 03:56:59.209 [debug] <0.287.0> Lager installed handler`),
		[]byte(`2026-05-26T04:17:13.300406Z [warning  ] [FF] flag evaluation failed`),
	}
	fs := &fakeSource{id: 99, name: "fake-text", lines: lines}
	if err := srv.attach(fs.id, fs, ""); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for srv.Store().Head() < uint64(len(lines)) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := srv.Store().Head(); got < uint64(len(lines)) {
		t.Fatalf("expected %d rows, got head=%d", len(lines), got)
	}

	// Use the same parser to derive expected values — keeps the test
	// TZ-independent (zone-less inputs are parsed in Local).
	for i, line := range lines {
		row := srv.Store().Materialize(uint64(i))
		if row == nil {
			t.Fatalf("row %d not materialized", i)
		}
		if !row.Text {
			t.Errorf("row %d: text flag = false, want true", i)
		}
		wantTs, wantLvl := source.ParseTextLine(string(line))
		if wantLvl == "" {
			t.Fatalf("row %d: test setup — ParseTextLine returned empty level for %q", i, line)
		}
		if row.Level != wantLvl {
			t.Errorf("row %d: level = %q, want %q", i, row.Level, wantLvl)
		}
		if math.Abs(row.Ts-wantTs) > 1e-6 {
			t.Errorf("row %d: ts = %.9f, want %.9f", i, row.Ts, wantTs)
		}
		// Msg preserved verbatim — the prefix is still there.
		if row.Msg != string(line) {
			t.Errorf("row %d: msg = %q, want unchanged %q", i, row.Msg, string(line))
		}
	}
}
