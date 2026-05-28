// Package client implements the local CLI client used by `loggi tail`,
// `loggi stdin`, etc. It dials the unix socket if present, otherwise spawns
// the server in daemon mode and retries.
package client

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/iOliverNguyen/loggi/internal/config"
	"github.com/iOliverNguyen/loggi/internal/frame"
	"github.com/iOliverNguyen/loggi/internal/wire"
)

// Conn is a unix-socket framed connection to the server.
type Conn struct {
	c   net.Conn
	wmu sync.Mutex
}

// Dial returns a connection to the server, auto-starting it if needed.
// If autoStart is false, returns ErrServerDown when no server is running.
func Dial(autoStart bool) (*Conn, error) {
	sockPath := config.SocketPath()

	if c, err := net.DialTimeout("unix", sockPath, 200*time.Millisecond); err == nil {
		return &Conn{c: c}, nil
	} else if !autoStart {
		return nil, ErrServerDown
	}

	// Before spawning a new daemon, see whether a loggi is already running
	// but using a non-default socket path (e.g. different $TMPDIR, or its
	// runtime.json got wiped). Discovering via /api/health lets us reuse
	// the running daemon instead of failing to spawn a duplicate.
	if h, _ := DiscoverRunningDaemon(); h != nil && h.Socket != "" && h.Socket != sockPath {
		if c, err := net.DialTimeout("unix", h.Socket, 200*time.Millisecond); err == nil {
			return &Conn{c: c}, nil
		}
		// Fall through to spawn — discovered socket isn't reachable from
		// this process (perms, deleted file). The existing spawn path will
		// then bind-fail and surface a clear "address already in use" hint.
	}

	// Acquire lock to avoid racing two clients into spawning two daemons.
	lockPath := config.LockPath()
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock: %w", err)
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return nil, fmt.Errorf("flock: %w", err)
	}

	// Re-dial after acquiring lock (another process may have started one).
	if c, err := net.DialTimeout("unix", sockPath, 200*time.Millisecond); err == nil {
		return &Conn{c: c}, nil
	}

	// Stale socket?
	_ = os.Remove(sockPath)

	// Note server.log's size before spawning so that, if the daemon dies
	// during startup, we can surface what it logged rather than a generic
	// timeout.
	logPath, _ := config.ServerLogFile()
	var logOffset int64
	if fi, statErr := os.Stat(logPath); statErr == nil {
		logOffset = fi.Size()
	}

	if err := spawnDaemon(); err != nil {
		return nil, err
	}

	// Poll-dial up to 3s.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("unix", sockPath, 200*time.Millisecond)
		if err == nil {
			return &Conn{c: c}, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	if reason := serverStartError(logPath, logOffset); reason != "" {
		return nil, fmt.Errorf("loggi server failed to start: %s", reason)
	}
	return nil, errors.New("server did not become reachable in time")
}

// serverStartError reads what the daemon appended to server.log since offset
// and returns the last error line, so a failed spawn surfaces its real reason
// (e.g. a port conflict) instead of a generic timeout. Returns "" if nothing
// useful was logged.
func serverStartError(logPath string, offset int64) string {
	data, err := os.ReadFile(logPath)
	if err != nil || int64(len(data)) <= offset {
		return ""
	}
	var reason string
	for _, ln := range bytes.Split(data[offset:], []byte("\n")) {
		if bytes.Contains(ln, []byte("rror:")) {
			reason = strings.TrimSpace(string(ln))
		}
	}
	// Drop the daemon's own "error:"/"Error:" prefix so the wrapped message
	// doesn't read "failed to start: error: …".
	reason = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(reason, "Error:"), "error:"))
	if strings.Contains(reason, "address already in use") {
		reason += " (another loggi may be running — try `loggi server stop`)"
	}
	return reason
}

// ErrServerDown is returned by Dial when autoStart is false and no server is up.
var ErrServerDown = errors.New("loggi server is not running")

// Read reads one server message.
func (c *Conn) Read(v *wire.ServerMsg) error { return frame.Read(c.c, v) }

// Write writes one client message.
func (c *Conn) Write(v *wire.ClientMsg) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return frame.Write(c.c, v)
}

// Close closes the connection.
func (c *Conn) Close() error { return c.c.Close() }

// Underlying exposes the raw net.Conn for advanced uses (e.g. piping bytes).
func (c *Conn) Underlying() io.ReadWriteCloser { return c.c }

func spawnDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if _, err := config.EnsureUserDir(); err != nil {
		return err
	}
	logPath, _ := config.ServerLogFile()
	logF, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)

	cmd := exec.Command(exe, "server", "--daemon")
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		if logF != nil {
			_ = logF.Close()
		}
		return fmt.Errorf("spawn daemon: %w", err)
	}
	// Detach: drop the os.Process handle so the kernel doesn't keep it
	// reapable by us. The child has its own session via Setsid.
	_ = cmd.Process.Release()
	// Close the parent's fd; the child has its own duplicated copy.
	if logF != nil {
		_ = logF.Close()
	}
	return nil
}
