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
	"github.com/iOliverNguyen/loggi/internal/wire"
)

// DefaultHTTPBind is the bind address used when Options.HTTPBind is empty.
// Kept fixed (not :0) so users can bookmark the SPA URL across restarts.
const DefaultHTTPBind = "127.0.0.1:9199"

// Options configures a Server.
type Options struct {
	SocketPath     string
	HTTPBind       string // e.g. "127.0.0.1:9199" — empty means default to DefaultHTTPBind
	IdleTimeout    time.Duration
	StoreCap       uint64
	Logger         *log.Logger
	StaticFS       http.Handler // embedded SPA handler; nil = serve a placeholder
	Profiles       []ProfileInfo
	Theme          string
	DefaultProfile string
	// RepoRoot is the detected repo root (.git/go.mod) at server-start cwd.
	// Empty if the server wasn't started inside a repo. Used to resolve the
	// "repo" save destination for /api/profiles.
	RepoRoot string
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
	sessionCnt atomic.Int64

	// Live session registry for fanning out source events to every
	// connected client (CLI socket + every web tab). Population is keyed by
	// session.id so we can unregister cleanly even if a session crashes
	// mid-handler. Writers to a session's conn never block one another:
	// each conn already serializes its writes via wmu.
	sessMu   sync.RWMutex
	sessions map[uint64]*session

	// profilesMu guards Options.Profiles after construction. The slice is
	// mutated by save/delete handlers and read by GET; without this the
	// reader would walk a backing array being rewritten in place.
	profilesMu sync.Mutex

	httpListener net.Listener
	unixListener net.Listener

	ctx    context.Context
	cancel context.CancelFunc

	httpURL string

	startedAt time.Time

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
	srv := &Server{
		opts:     opts,
		logger:   opts.Logger,
		store:    store.New(store.Options{Cap: opts.StoreCap}),
		srcs:     make(map[uint64]*sourceRec),
		ingest:   make(chan source.RawLine, 8192),
		ctx:      ctx,
		cancel:   cancel,
		sessions: make(map[uint64]*session),
	}
	srv.store.SetSourceNameLookup(func(id uint64) string {
		srv.srcMu.RLock()
		defer srv.srcMu.RUnlock()
		if r, ok := srv.srcs[id]; ok {
			return r.src.Name()
		}
		return ""
	})
	return srv
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
	s.startedAt = time.Now()
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
		bind = DefaultHTTPBind
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

// AddFileSource adds a tail-file source. Idempotent: if an open source with
// the same path already exists, returns its id without creating a duplicate.
func (s *Server) AddFileSource(path string) (uint64, error) {
	if id, ok := s.findOpenSource(source.KindFile, path); ok {
		return id, nil
	}
	id := s.srcGen.Next()
	src := newFileSource(id, path)
	return id, s.attach(id, src)
}

// AddStdinSource adds a stdin-forwarded source. Returns the source id and a
// pointer to the underlying stdin source so the calling session can push data.
// Stdin sources are not deduped: each pipe deserves its own ingest stream.
func (s *Server) AddStdinSource(name string) (uint64, *stdinSource, error) {
	id := s.srcGen.Next()
	src := newStdinSource(id, name)
	if err := s.attach(id, src); err != nil {
		return 0, nil, err
	}
	return id, src, nil
}

// AddDockerSource adds a docker container source. Idempotent on container
// name: re-clicking "Add" returns the existing id without spawning a parallel
// tail. Initial backfill is fixed at 300 lines from the engine; older entries
// flow through the store ring buffer via the History RPC.
func (s *Server) AddDockerSource(name string) (uint64, error) {
	if id, ok := s.findOpenSource(source.KindDocker, name); ok {
		return id, nil
	}
	id := s.srcGen.Next()
	src, err := newDockerSource(id, name)
	if err != nil {
		return 0, err
	}
	return id, s.attach(id, src)
}

// findOpenSource returns the id of an open source matching (kind, identifier)
// — path for file, container name for docker. Used to dedupe Add*Source calls
// so re-clicking "Add" or racing reconnects don't spawn parallel tails of the
// same target.
func (s *Server) findOpenSource(kind source.Kind, ident string) (uint64, bool) {
	s.srcMu.RLock()
	defer s.srcMu.RUnlock()
	for id, r := range s.srcs {
		if r.state != "open" {
			continue
		}
		if r.src.Kind() == kind && r.src.Name() == ident {
			return id, true
		}
	}
	return 0, false
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
		var newState, detail string
		s.srcMu.Lock()
		if r, ok := s.srcs[id]; ok {
			if err != nil && err != context.Canceled {
				r.state = "error"
				newState = "error"
				detail = err.Error()
				s.logger.Printf("source %d (%s) error: %v", id, src.Name(), err)
			} else {
				r.state = "closed"
				newState = "closed"
			}
		}
		s.srcMu.Unlock()
		if newState != "" {
			s.broadcastSourceState(id, newState, detail)
		}
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
	mode, justSet := s.detectMode(line.SourceID, line.Bytes)
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
	if justSet {
		// Broadcast so clients learn the mode without needing a fresh snapshot.
		s.broadcastSourceState(line.SourceID, "open", "")
	}
}

// detectMode caches a per-source decision: on the first ingested line for a
// source, decide JSON vs text. Once decided, mode is sticky and read-only.
// rec.mode is an atomic.Pointer so the per-line check is uncontended.
// justSet is true only for the goroutine that won the CAS — used by the
// caller to broadcast a one-time mode-update event.
func (s *Server) detectMode(id uint64, line []byte) (mode string, justSet bool) {
	s.srcMu.RLock()
	rec, ok := s.srcs[id]
	s.srcMu.RUnlock()
	if !ok {
		// Anonymous; try JSON.
		if isJSONObj(line) {
			return "json", false
		}
		return "text", false
	}
	if m := rec.mode.Load(); m != nil {
		return *m, false
	}
	m := "text"
	if isJSONObj(line) {
		m = "json"
	}
	if rec.mode.CompareAndSwap(nil, &m) {
		return m, true
	}
	// Lost the CAS race; re-read what the winner stored.
	return *rec.mode.Load(), false
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
	return open == 0 && s.sessionCnt.Load() == 0
}

// registerSession adds sess to the live registry so source events fan out
// to its conn. Idempotent on id collisions (the sessionGen is a single
// monotonic counter, so collisions don't happen in practice).
func (s *Server) registerSession(sess *session) {
	s.sessMu.Lock()
	s.sessions[sess.id] = sess
	s.sessMu.Unlock()
}

// unregisterSession is called on session exit. Safe to call for ids that
// were never registered (no-op).
func (s *Server) unregisterSession(id uint64) {
	s.sessMu.Lock()
	delete(s.sessions, id)
	s.sessMu.Unlock()
}

// broadcastSourceEvent sends ev to every registered session. Per-conn
// write errors are non-fatal — a stuck client will be reaped by its own
// read loop. We snapshot the session list under RLock so a slow Write
// doesn't hold up registry mutation.
func (s *Server) broadcastSourceEvent(ev wire.SourceEvent) {
	s.sessMu.RLock()
	sessList := make([]*session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessList = append(sessList, sess)
	}
	s.sessMu.RUnlock()
	msg := &wire.ServerMsg{Type: wire.SMsgSource, Source: &ev}
	for _, sess := range sessList {
		_ = sess.conn.Write(msg)
	}
}

// broadcastSourceState builds a SourceEvent from the current source
// record (if it still exists) and broadcasts it. Used both for "open"
// transitions from session.go and for async "error"/"closed" transitions
// from the attach goroutine.
func (s *Server) broadcastSourceState(id uint64, state, detail string) {
	ev := wire.SourceEvent{SourceID: id, State: state, Detail: detail}
	s.srcMu.RLock()
	if r, ok := s.srcs[id]; ok {
		ev.Kind = string(r.src.Kind())
		ev.Name = r.src.Name()
		ev.Mode = r.modeStr()
	}
	s.srcMu.RUnlock()
	s.broadcastSourceEvent(ev)
}

// Done returns a channel that closes when the server is shut down.
func (s *Server) Done() <-chan struct{} { return s.ctx.Done() }
