package server

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iOliverNguyen/loggi/internal/frame"
	"github.com/iOliverNguyen/loggi/internal/store"
	"github.com/iOliverNguyen/loggi/internal/wire"
)

func makeJSON(i int) []byte {
	return fmt.Appendf(nil, `{"level":"info","ts":%d,"service":"x","msg":"row %d"}`, i+1, i)
}

func makeJSONLevel(i int, level string) []byte {
	return fmt.Appendf(nil, `{"level":%q,"ts":%d,"service":"x","msg":"row %d"}`, level, i+1, i)
}

func toAppendInput(i int) store.AppendInput {
	return store.AppendInput{SourceID: 1, JSON: makeJSON(i)}
}

func toAppendInputLevel(i int, level string) store.AppendInput {
	return store.AppendInput{SourceID: 1, JSON: makeJSONLevel(i, level)}
}

// TestAddFileSourceDedupe verifies that AddFileSource is idempotent on path
// for currently-open sources, and that re-adding after RemoveSource yields a
// fresh id (the previous mapping is no longer "open").
func TestAddFileSourceDedupe(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "x.log")
	if err := os.WriteFile(logPath, []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(Options{
		HTTPBind:    "127.0.0.1:0",
		IdleTimeout: time.Hour,
		StoreCap:    1024,
	})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()

	id1, err := srv.AddFileSource(logPath)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := srv.AddFileSource(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("expected dedupe to return same id, got %d then %d", id1, id2)
	}

	// After remove, a re-add must allocate a new id.
	if err := srv.RemoveSource(id1); err != nil {
		t.Fatal(err)
	}
	id3, err := srv.AddFileSource(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if id3 == id1 {
		t.Fatalf("expected fresh id after remove, got same id %d", id3)
	}
}

// TestHistoryRPC subscribes, ingests N rows, then issues a History RPC for
// rows older than a chosen seq and verifies they come back with IsHistory=true.
func TestHistoryRPC(t *testing.T) {
	tmp := t.TempDir()
	sockPath := filepath.Join(tmp, "loggi.sock")
	srv := NewServer(Options{
		SocketPath:  sockPath,
		HTTPBind:    "127.0.0.1:0",
		IdleTimeout: time.Hour,
		StoreCap:    1024,
	})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()

	// Seed the store with 50 rows directly.
	for i := range 50 {
		srv.Store().Publish(toAppendInput(i))
	}

	c, err := net.DialTimeout("unix", sockPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// Drain initial snapshot.
	var sm wire.ServerMsg
	if err := frame.Read(c, &sm); err != nil {
		t.Fatal(err)
	}

	// Subscribe with no backfill.
	if err := frame.Write(c, &wire.ClientMsg{
		Type: wire.CMsgSubscribe,
		ID:   1,
		Subscribe: &wire.Subscribe{SubID: 9, Filter: "", HistoryN: 0},
	}); err != nil {
		t.Fatal(err)
	}
	// Drain ack.
	for range 5 {
		_ = c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		var m wire.ServerMsg
		if err := frame.Read(c, &m); err != nil {
			break
		}
		if m.Type == wire.SMsgAck {
			break
		}
	}

	// History request: ask for rows before seq 30, limit 10.
	if err := frame.Write(c, &wire.ClientMsg{
		Type: wire.CMsgHistory,
		ID:   2,
		History: &wire.History{SubID: 9, BeforeSeq: 30, Limit: 10},
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	gotHistory := false
	for time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		var m wire.ServerMsg
		if err := frame.Read(c, &m); err != nil {
			continue
		}
		if m.Type == wire.SMsgBatch && m.Batch != nil && m.Batch.IsHistory {
			gotHistory = true
			if len(m.Batch.Entries) != 10 {
				t.Fatalf("history: want 10 entries got %d", len(m.Batch.Entries))
			}
			// All seqs must be < 30 (BeforeSeq is exclusive) and the last
			// entry should be just under the boundary (seqs are 1-indexed).
			last := m.Batch.Entries[len(m.Batch.Entries)-1].Seq
			if last >= 30 {
				t.Fatalf("history: last seq %d should be < 30", last)
			}
			break
		}
	}
	if !gotHistory {
		t.Fatalf("did not receive history batch within deadline")
	}
}

// TestSetFilterBackfill verifies that updating the filter on an existing
// subscription causes the server to resend matching backlog (so the client
// doesn't lose all visible rows on filter change).
func TestSetFilterBackfill(t *testing.T) {
	tmp := t.TempDir()
	sockPath := filepath.Join(tmp, "loggi.sock")
	srv := NewServer(Options{
		SocketPath:  sockPath,
		HTTPBind:    "127.0.0.1:0",
		IdleTimeout: time.Hour,
		StoreCap:    1024,
	})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()

	// Seed with mixed levels.
	for i := range 20 {
		level := "info"
		if i%2 == 0 {
			level = "error"
		}
		srv.Store().Publish(toAppendInputLevel(i, level))
	}

	c, err := net.DialTimeout("unix", sockPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// Drain snapshot.
	var sm wire.ServerMsg
	if err := frame.Read(c, &sm); err != nil {
		t.Fatal(err)
	}

	// Subscribe with HistoryN=20 (everything matches).
	_ = frame.Write(c, &wire.ClientMsg{
		Type: wire.CMsgSubscribe,
		ID:   1,
		Subscribe: &wire.Subscribe{SubID: 5, Filter: "", HistoryN: 20},
	})

	// Drain initial backlog.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		var m wire.ServerMsg
		if err := frame.Read(c, &m); err != nil {
			break
		}
		if m.Type == wire.SMsgBatch && m.Batch != nil && len(m.Batch.Entries) > 0 {
			break
		}
	}

	// Update filter to level:error.
	_ = frame.Write(c, &wire.ClientMsg{
		Type: wire.CMsgFilter,
		ID:   2,
		Filter: &wire.UpdateFilter{SubID: 5, Filter: "level:error"},
	})

	// Expect a backlog batch matching the new filter (10 errors).
	gotMatching := 0
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		var m wire.ServerMsg
		if err := frame.Read(c, &m); err != nil {
			continue
		}
		if m.Type == wire.SMsgBatch && m.Batch != nil {
			for _, e := range m.Batch.Entries {
				if strings.EqualFold(e.Level, "error") {
					gotMatching++
				}
			}
			if gotMatching >= 5 {
				break
			}
		}
	}
	if gotMatching == 0 {
		t.Fatalf("expected backfill of error rows after SetFilter, got 0")
	}
}
