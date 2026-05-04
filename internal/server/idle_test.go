package server

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// shortSocketPath returns a /tmp-rooted path short enough for sun_path
// (104 bytes on macOS, 108 on Linux). t.TempDir() can be too long on macOS.
var sockSeq atomic.Uint64

func shortSocketPath(t *testing.T) string {
	t.Helper()
	n := sockSeq.Add(1)
	p := filepath.Join(os.TempDir(), fmt.Sprintf("loggi-test-%d-%d.sock", os.Getpid(), n))
	t.Cleanup(func() { _ = os.Remove(p) })
	return p
}

// TestIdleExitFires verifies the daemon exits via the idle timer when no
// sources and no clients are active. Uses a short timeout (200 ms).
func TestIdleExitFires(t *testing.T) {
	srv := NewServer(Options{
		SocketPath:  shortSocketPath(t),
		HTTPBind:    "127.0.0.1:0",
		IdleTimeout: 200 * time.Millisecond,
		StoreCap:    1024,
	})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()

	select {
	case <-srv.Done():
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("idle exit did not fire within 2s (configured 200 ms)")
	}
}

// TestIdleExitCanceledByActivity: an active source cancels the idle timer;
// the server stays up.
func TestIdleExitCanceledByActivity(t *testing.T) {
	tmp := t.TempDir()
	srv := NewServer(Options{
		SocketPath:  shortSocketPath(t),
		HTTPBind:    "127.0.0.1:0",
		IdleTimeout: 300 * time.Millisecond,
		StoreCap:    1024,
	})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()

	tmpFile := filepath.Join(tmp, "x.log")
	if err := os.WriteFile(tmpFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.AddFileSource(tmpFile); err != nil {
		t.Fatal(err)
	}

	select {
	case <-srv.Done():
		t.Fatal("server shut down despite active source")
	case <-time.After(500 * time.Millisecond):
		// good — survived past the idle window
	}
}
