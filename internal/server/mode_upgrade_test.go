package server

import (
	"context"
	"testing"
	"time"

	"github.com/iOliverNguyen/loggi/internal/source"
)

// fakeSource emits a fixed list of byte lines on Run, then blocks on ctx.
type fakeSource struct {
	id    uint64
	name  string
	lines [][]byte
}

func (f *fakeSource) ID() uint64        { return f.id }
func (f *fakeSource) Kind() source.Kind { return source.KindDocker }
func (f *fakeSource) Name() string      { return f.name }
func (f *fakeSource) Close() error      { return nil }

func (f *fakeSource) Run(ctx context.Context, out chan<- source.RawLine) error {
	for _, b := range f.lines {
		cp := make([]byte, len(b))
		copy(cp, b)
		select {
		case out <- source.RawLine{SourceID: f.id, Bytes: cp}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

// TestModeUpgradeAfterNonJSONPrelude reproduces the connectly-goapps bug:
// a Docker source whose first lines are a non-JSON stack trace must not
// pin to text mode forever. Once a JSON line arrives, the source's UI
// mode must upgrade to "json" and the JSON line must be parsed (fields
// extracted, level populated) instead of being stored as raw msg.
func TestModeUpgradeAfterNonJSONPrelude(t *testing.T) {
	srv := NewServer(Options{
		HTTPBind:    "127.0.0.1:0",
		IdleTimeout: time.Hour,
		StoreCap:    1024,
	})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()

	fs := &fakeSource{
		id:   42,
		name: "fake",
		lines: [][]byte{
			[]byte("\t/code/vendor/github.com/robfig/cron/v3/chain.go:53\r"),
			[]byte("github.com/robfig/cron/v3.FuncJob.Run\r"),
			[]byte(`{"level":"debug","ts":1778485655.336,"service":"api","msg":"hello"}`),
		},
	}
	if err := srv.attach(fs.id, fs, ""); err != nil {
		t.Fatal(err)
	}

	// Wait until all three lines are ingested.
	deadline := time.Now().Add(2 * time.Second)
	for srv.Store().Head() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := srv.Store().Head(); got < 3 {
		t.Fatalf("expected at least 3 rows ingested, got head=%d", got)
	}

	// Source mode badge must have upgraded to "json".
	srcs := srv.Sources()
	var found bool
	for _, s := range srcs {
		if s.ID == fs.id {
			found = true
			if s.Mode != "json" {
				t.Errorf("source mode = %q, want %q after JSON line arrived", s.Mode, "json")
			}
		}
	}
	if !found {
		t.Fatalf("source %d not present in Sources()", fs.id)
	}

	// Row 0 + 1 are text (stack trace). Row 2 must be parsed JSON:
	// level == "debug", non-zero ts, and msg == "hello" (not the
	// raw envelope).
	row := srv.Store().Materialize(2)
	if row == nil {
		t.Fatal("row 2 not materialized")
	}
	if row.Text {
		t.Errorf("row 2 stored as text, expected JSON-parsed")
	}
	if row.Level != "debug" {
		t.Errorf("row 2 level = %q, want %q", row.Level, "debug")
	}
	if row.Msg != "hello" {
		t.Errorf("row 2 msg = %q, want %q", row.Msg, "hello")
	}
	if row.Ts == 0 {
		t.Errorf("row 2 ts = 0, expected the parsed JSON ts")
	}

	// And row 0 must still be text (the routing per-line is honest
	// about non-JSON lines, even after the upgrade).
	row0 := srv.Store().Materialize(0)
	if row0 == nil {
		t.Fatal("row 0 not materialized")
	}
	if !row0.Text {
		t.Errorf("row 0 should be text (stack trace), got JSON-parsed")
	}
}
