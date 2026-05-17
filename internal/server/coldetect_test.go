package server

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iOliverNguyen/loggi/internal/frame"
	"github.com/iOliverNguyen/loggi/internal/wire"
)

// shortSockPath returns a unix-socket-safe path under /tmp. macOS limits
// unix socket paths to ~104 chars; the default t.TempDir() under
// /var/folders/... exceeds that. We use /tmp + a random suffix so
// parallel tests don't collide, and register cleanup via t.Cleanup.
func shortSockPath(t *testing.T) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	p := filepath.Join("/tmp", "loggi-test-"+hex.EncodeToString(b[:])+".sock")
	os.Remove(p)
	t.Cleanup(func() { os.Remove(p) })
	return p
}

// TestColumnDetection_Samples drives the three reference log shapes
// through the full server pipeline and verifies the SourceEvent.Columns
// broadcast contains the canonical priority ids. Column detection runs
// on the ingester goroutine after a JSON line arrives; the sampler
// closes when it hits its entry limit (set tight here so the small
// fixtures fire detection).
func TestColumnDetection_Samples(t *testing.T) {
	cases := []struct {
		name string
		file string
		want []string // ids that must be present in the broadcast
	}{
		{name: "go", file: "log-go.jsonl", want: []string{"ts", "level", "msg", "service", "caller"}},
		{name: "nodejs", file: "log-nodejs.jsonl", want: []string{"ts", "level", "msg", "service", "caller"}},
		{name: "python", file: "log-py.jsonl", want: []string{"ts", "level", "msg", "service", "caller"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cols := runDetectionRoundTrip(t, tc.file)
			for _, want := range tc.want {
				if !slices.Contains(cols, want) {
					t.Errorf("missing %q in detected columns: %v", want, cols)
				}
			}
		})
	}
}

// runDetectionRoundTrip writes the named fixture into a tmp file under an
// isolated HOME (so SourcePref persistence doesn't touch the developer's
// real config), spins up a server with tight sampler bounds, adds the
// file source, and returns the SourceEvent.Columns the server broadcasts
// when detection closes.
func runDetectionRoundTrip(t *testing.T, fixture string) []string {
	t.Helper()
	// Pin XDG / HOME so persistSourcePref writes into a tmp dir.
	t.Setenv("HOME", t.TempDir())

	tmp := t.TempDir()
	dst := filepath.Join(tmp, fixture)
	src := filepath.Join("..", "..", "_docs", "sample", fixture)
	srcData, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// Count non-blank JSON lines to size the sampler limit precisely.
	nLines := 0
	scn := bufio.NewScanner(strings.NewReader(string(srcData)))
	for scn.Scan() {
		l := strings.TrimSpace(scn.Text())
		if l != "" && l[0] == '{' {
			nLines++
		}
	}
	if nLines == 0 {
		t.Fatalf("fixture %s has no JSON lines", src)
	}
	if err := os.WriteFile(dst, srcData, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// Unix sockets cap at 104 chars on macOS; t.TempDir paths under
	// /var/folders exceed that. Use a short /tmp path scoped to this test.
	sockPath := shortSockPath(t)
	srv := NewServer(Options{
		SocketPath:          sockPath,
		HTTPBind:            "127.0.0.1:0",
		IdleTimeout:         time.Hour,
		StoreCap:            1024,
		ColumnDetectLimit:   nLines,         // close right at end of fixture
		ColumnDetectTimeout: 5 * time.Second, // backstop
		FilePollMS:          20,
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	t.Cleanup(srv.Shutdown)

	// Connect first so we capture the live SourceEvent the server emits
	// when detection closes. The snapshot also carries Columns, but only
	// if detection had already happened before the client subscribed.
	c, err := net.DialTimeout("unix", sockPath, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	// Drain the initial snapshot before adding the source.
	var sm wire.ServerMsg
	if err := frame.Read(c, &sm); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if sm.Type != wire.SMsgSnapshot {
		t.Fatalf("first frame: want snapshot got %s", sm.Type)
	}

	if _, err := srv.AddFileSource(dst); err != nil {
		t.Fatalf("add file source: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		var m wire.ServerMsg
		if err := frame.Read(c, &m); err != nil {
			continue
		}
		if m.Type == wire.SMsgSource && m.Source != nil && len(m.Source.Columns) > 0 {
			return m.Source.Columns
		}
	}
	t.Fatalf("never received SourceEvent with Columns")
	return nil
}

// TestColumnDetection_APIColumns verifies the /api/columns endpoint
// exposes the persisted by_source map after a detection cycle. Drives
// the same fixture through, then GETs the endpoint and asserts the
// "kind:name" key is populated.
func TestColumnDetection_APIColumns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "log.jsonl")
	src := filepath.Join("..", "..", "_docs", "sample", "log-py.jsonl")
	srcData, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(dst, srcData, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// Count lines so sampler closes at end-of-file.
	nLines := strings.Count(string(srcData), "\n")
	if nLines == 0 {
		t.Fatalf("empty fixture")
	}

	// Unix sockets cap at 104 chars on macOS; t.TempDir paths under
	// /var/folders exceed that. Use a short /tmp path scoped to this test.
	sockPath := shortSockPath(t)
	srv := NewServer(Options{
		SocketPath:          sockPath,
		HTTPBind:            "127.0.0.1:0",
		IdleTimeout:         time.Hour,
		StoreCap:            1024,
		ColumnDetectLimit:   nLines,
		ColumnDetectTimeout: 5 * time.Second,
		FilePollMS:          20,
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	t.Cleanup(srv.Shutdown)

	if _, err := srv.AddFileSource(dst); err != nil {
		t.Fatalf("add file source: %v", err)
	}

	// Poll /api/columns until the by_source key appears (detection runs
	// asynchronously on the ingester goroutine; we give it up to 5s).
	wantKey := "file:" + dst
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(srv.HTTPURL() + "/api/columns")
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		body := make([]byte, 4096)
		n, _ := resp.Body.Read(body)
		resp.Body.Close()
		s := string(body[:n])
		if strings.Contains(s, `"`+wantKey+`"`) {
			// Got it. Verify the canonical "ts" id is in the list to
			// confirm we're seeing the detection-driven entry rather
			// than a stale empty map.
			if !strings.Contains(s, `"ts"`) {
				t.Fatalf("by_source[%s] missing ts: %s", wantKey, s)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("by_source[%s] never appeared in /api/columns", wantKey)
}
