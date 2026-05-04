package server

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/iOliverNguyen/loggi/internal/frame"
	"github.com/iOliverNguyen/loggi/internal/wire"
)

// TestEndToEndUnixSocket starts a server, tails a tiny log file, and verifies
// a subscriber receives a batch with the expected entries.
func TestEndToEndUnixSocket(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "test.jsonl")
	if err := os.WriteFile(logPath, []byte(`{"level":"info","ts":1,"service":"x","msg":"hello"}
{"level":"error","ts":2,"service":"x","msg":"boom"}
`), 0o600); err != nil {
		t.Fatal(err)
	}

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

	// Add the file source directly via server API.
	if _, err := srv.AddFileSource(logPath); err != nil {
		t.Fatal(err)
	}

	// Connect to the unix socket as a client.
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
	if sm.Type != wire.SMsgSnapshot {
		t.Fatalf("first frame: want snapshot got %s", sm.Type)
	}

	// Subscribe with history.
	if err := frame.Write(c, &wire.ClientMsg{
		Type: wire.CMsgSubscribe,
		ID:   1,
		Subscribe: &wire.Subscribe{
			SubID:    7,
			Filter:   "",
			HistoryN: 100,
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Read until we get a batch (which should contain the historical entries).
	deadline := time.Now().Add(3 * time.Second)
	got := 0
	for time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		var m wire.ServerMsg
		if err := frame.Read(c, &m); err != nil {
			continue
		}
		if m.Type == wire.SMsgBatch && m.Batch != nil {
			got += len(m.Batch.Entries)
			if got >= 2 {
				break
			}
		}
	}
	if got < 2 {
		t.Fatalf("want >=2 entries got %d", got)
	}
}
