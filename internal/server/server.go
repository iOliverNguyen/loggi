// Package server runs the loggi backend: ingester goroutine, store, hub of
// subscribers, unix-socket listener, and HTTP/WebSocket listener.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iOliverNguyen/loggi/internal/config"
	"github.com/iOliverNguyen/loggi/internal/source"
	"github.com/iOliverNguyen/loggi/internal/store"
)

// Options configures a Server.
type Options struct {
	SocketPath     string
	HTTPBind       string // e.g. "127.0.0.1:0"
	IdleTimeout    time.Duration
	StoreCap       uint64
	Logger         *log.Logger
	StaticFS       http.Handler // embedded SPA handler; nil = serve a placeholder
	Profiles       []ProfileInfo
	Theme          string
	DefaultProfile string
}

// ProfileInfo is the wire representation of a config profile.
type ProfileInfo struct {
	Name    string   `json:"name"`
	Filter  string   `json:"filter"`
	Columns []string `json:"columns,omitempty"`
}

// Server is the main entrypoint for the loggi server.
type Server struct {
	opts Options

	logger *log.Logger
	store  *store.Store

	// Source registry and IDs.
	srcMu  sync.RWMutex
	srcs   map[uint64]*sourceRec
	srcGen source.IDGen

	// Ingest fan-in channel.
	ingest chan source.RawLine

	// Client session count for idle exit.
	sessions atomic.Int64

	httpListener net.Listener
	unixListener net.Listener

	ctx    context.Context
	cancel context.CancelFunc

	httpURL string

	// Idle timer
	idleMu    sync.Mutex
	idleTimer *time.Timer
}

type sourceRec struct {
	src    source.Source
	cancel context.CancelFunc
	// mode is set once on first ingested line; readers use atomic load to
	// avoid contention with the per-line ingester check. nil means
	// undetected.
	mode  atomic.Pointer[string]
	state string // "open" | "closed" | "error" — guarded by Server.srcMu
}

// modeStr returns the current mode or "" if undetected.
func (r *sourceRec) modeStr() string {
	p := r.mode.Load()
	if p == nil {
		return ""
	}
	return *p
}

// NewServer constructs a Server.
func NewServer(opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = log.New(os.Stderr, "", log.LstdFlags|log.Lmicroseconds)
	}
	if opts.IdleTimeout == 0 {
		opts.IdleTimeout = 5 * time.Minute
	}
	if opts.StoreCap == 0 {
		opts.StoreCap = 524288
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		opts:   opts,
		logger: opts.Logger,
		store:  store.New(store.Options{Cap: opts.StoreCap}),
		srcs:   make(map[uint64]*sourceRec),
		ingest: make(chan source.RawLine, 8192),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Store returns the underlying store (used by tests / handlers).
func (s *Server) Store() *store.Store { return s.store }

// HTTPURL returns the chosen HTTP base URL after Start has run.
func (s *Server) HTTPURL() string { return s.httpURL }

// SocketPath returns the unix socket path.
func (s *Server) SocketPath() string { return s.opts.SocketPath }

// Start begins listening on the unix socket and HTTP port and runs the
// ingester loop. It returns once both listeners are ready; serving continues
// in goroutines until Shutdown is called.
func (s *Server) Start() error {
	// Unix socket
	if s.opts.SocketPath != "" {
		_ = os.Remove(s.opts.SocketPath)
		l, err := net.Listen("unix", s.opts.SocketPath)
		if err != nil {
			return fmt.Errorf("listen unix: %w", err)
		}
		_ = os.Chmod(s.opts.SocketPath, 0o600)
		s.unixListener = l
		go s.serveUnix(l)
	}
	// HTTP / WebSocket
	bind := s.opts.HTTPBind
	if bind == "" {
		bind = "127.0.0.1:0"
	}
	hl, err := net.Listen("tcp", bind)
	if err != nil {
		return fmt.Errorf("listen tcp: %w", err)
	}
	s.httpListener = hl
	addr := hl.Addr().(*net.TCPAddr)
	s.httpURL = "http://" + addr.IP.String() + ":" + strconv.Itoa(addr.Port)
	go s.serveHTTP(hl)

	go s.ingester()

	s.startIdleTimer()
	return nil
}

// Shutdown stops listeners, sources, and the ingester.
func (s *Server) Shutdown() {
	s.cancel()
	if s.unixListener != nil {
		_ = s.unixListener.Close()
	}
	if s.httpListener != nil {
		_ = s.httpListener.Close()
	}
	s.srcMu.Lock()
	for _, rec := range s.srcs {
		_ = rec.src.Close()
		rec.cancel()
	}
	s.srcs = nil
	s.srcMu.Unlock()
	s.store.Close()
	if s.opts.SocketPath != "" {
		_ = os.Remove(s.opts.SocketPath)
	}
	if rt, err := config.RuntimeFile(); err == nil {
		_ = os.Remove(rt)
	}
	if pf := config.PidPath(); pf != "" {
		_ = os.Remove(pf)
	}
}

// AddFileSource adds a tail-file source.
func (s *Server) AddFileSource(path string) (uint64, error) {
	id := s.srcGen.Next()
	src := newFileSource(id, path)
	return id, s.attach(id, src)
}

// AddStdinSource adds a stdin-forwarded source. Returns the source id and a
// pointer to the underlying stdin source so the calling session can push data.
func (s *Server) AddStdinSource(name string) (uint64, *stdinSource, error) {
	id := s.srcGen.Next()
	src := newStdinSource(id, name)
	if err := s.attach(id, src); err != nil {
		return 0, nil, err
	}
	return id, src, nil
}

// AddDockerSource adds a docker container source.
func (s *Server) AddDockerSource(name, since string) (uint64, error) {
	id := s.srcGen.Next()
	src, err := newDockerSource(id, name, since)
	if err != nil {
		return 0, err
	}
	return id, s.attach(id, src)
}

func (s *Server) attach(id uint64, src source.Source) error {
	ctx, cancel := context.WithCancel(s.ctx)
	rec := &sourceRec{src: src, cancel: cancel, state: "open"}
	s.srcMu.Lock()
	s.srcs[id] = rec
	s.srcMu.Unlock()
	s.cancelIdle()
	go func() {
		err := src.Run(ctx, s.ingest)
		s.srcMu.Lock()
		if r, ok := s.srcs[id]; ok {
			if err != nil && err != context.Canceled {
				r.state = "error"
				s.logger.Printf("source %d (%s) error: %v", id, src.Name(), err)
			} else {
				r.state = "closed"
			}
		}
		s.srcMu.Unlock()
		s.maybeStartIdle()
	}()
	return nil
}

// RemoveSource cancels a source.
func (s *Server) RemoveSource(id uint64) error {
	s.srcMu.Lock()
	rec, ok := s.srcs[id]
	if ok {
		delete(s.srcs, id)
	}
	s.srcMu.Unlock()
	if !ok {
		return fmt.Errorf("no such source %d", id)
	}
	_ = rec.src.Close()
	rec.cancel()
	s.maybeStartIdle()
	return nil
}

// Sources lists current sources.
func (s *Server) Sources() []SourceInfo {
	s.srcMu.RLock()
	defer s.srcMu.RUnlock()
	out := make([]SourceInfo, 0, len(s.srcs))
	for id, r := range s.srcs {
		out = append(out, SourceInfo{
			ID:    id,
			Kind:  string(r.src.Kind()),
			Name:  r.src.Name(),
			Mode:  r.modeStr(),
			State: r.state,
		})
	}
	return out
}

// SourceInfo is the snapshot for a source.
type SourceInfo struct {
	ID    uint64 `json:"id"`
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Mode  string `json:"mode"`
	State string `json:"state"`
}

// ingester is the single goroutine that decodes raw lines into store rows.
func (s *Server) ingester() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case line := <-s.ingest:
			s.processLine(line)
		}
	}
}

func (s *Server) processLine(line source.RawLine) {
	mode := s.detectMode(line.SourceID, line.Bytes)
	switch mode {
	case "json":
		s.store.Publish(store.AppendInput{SourceID: line.SourceID, JSON: line.Bytes})
	default:
		ansi, plain := stripANSI(line.Bytes)
		s.store.Publish(store.AppendInput{
			SourceID: line.SourceID,
			Text:     plain,
			AnsiBlob: ansi,
			Level:    detectLevelHint(plain),
		})
	}
}

// detectMode caches a per-source decision: on the first ingested line for a
// source, decide JSON vs text. Once decided, mode is sticky and read-only.
// rec.mode is an atomic.Pointer so the per-line check is uncontended.
func (s *Server) detectMode(id uint64, line []byte) string {
	s.srcMu.RLock()
	rec, ok := s.srcs[id]
	s.srcMu.RUnlock()
	if !ok {
		// Anonymous; try JSON.
		if isJSONObj(line) {
			return "json"
		}
		return "text"
	}
	if m := rec.mode.Load(); m != nil {
		return *m
	}
	mode := "text"
	if isJSONObj(line) {
		mode = "json"
	}
	rec.mode.CompareAndSwap(nil, &mode)
	// Re-load: another goroutine may have CAS'd a different value first.
	return *rec.mode.Load()
}

func isJSONObj(b []byte) bool {
	for _, c := range b {
		if c == ' ' || c == '\t' {
			continue
		}
		if c != '{' {
			return false
		}
		break
	}
	// Try parse just the keys.
	var v map[string]json.RawMessage
	if err := json.Unmarshal(b, &v); err != nil {
		return false
	}
	for _, k := range []string{"level", "msg", "ts", "message", "@timestamp"} {
		if _, ok := v[k]; ok {
			return true
		}
	}
	return false
}

func detectLevelHint(line string) string {
	upper := strings.ToUpper(line)
	for _, lvl := range []string{"TRACE", "DEBUG", "INFO", "WARN", "ERROR", "FATAL"} {
		if strings.Contains(upper, " "+lvl+" ") || strings.HasPrefix(upper, lvl+" ") {
			return strings.ToLower(lvl)
		}
	}
	return ""
}

// Idle timer: when no clients and no sources, start a timer; on expiry,
// re-check (a session may have arrived since the timer was scheduled — Stop
// can return false if the AfterFunc goroutine has already started). Only
// shut down if still genuinely idle.
func (s *Server) armIdleLocked() {
	if s.idleTimer != nil {
		return
	}
	if !s.shouldIdle() {
		return
	}
	t := time.AfterFunc(s.opts.IdleTimeout, s.idleFire)
	s.idleTimer = t
}

func (s *Server) idleFire() {
	s.idleMu.Lock()
	// Was this timer canceled or replaced?
	s.idleTimer = nil
	s.idleMu.Unlock()
	if !s.shouldIdle() {
		return
	}
	s.logger.Printf("idle timeout reached; shutting down")
	s.Shutdown()
}

func (s *Server) startIdleTimer() {
	s.idleMu.Lock()
	defer s.idleMu.Unlock()
	s.armIdleLocked()
}

func (s *Server) cancelIdle() {
	s.idleMu.Lock()
	defer s.idleMu.Unlock()
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
}

func (s *Server) maybeStartIdle() {
	s.idleMu.Lock()
	defer s.idleMu.Unlock()
	s.armIdleLocked()
}

func (s *Server) shouldIdle() bool {
	s.srcMu.RLock()
	open := 0
	for _, r := range s.srcs {
		if r.state == "open" {
			open++
		}
	}
	s.srcMu.RUnlock()
	return open == 0 && s.sessions.Load() == 0
}

// Done returns a channel that closes when the server is shut down.
func (s *Server) Done() <-chan struct{} { return s.ctx.Done() }
