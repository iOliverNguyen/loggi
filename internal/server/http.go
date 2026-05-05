package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/iOliverNguyen/loggi/internal/config"
	"github.com/iOliverNguyen/loggi/internal/source/docker"
	"github.com/iOliverNguyen/loggi/internal/wire"
)

// wsConn adapts a websocket connection to Conn (JSON in/out).
type wsConn struct {
	c   *websocket.Conn
	ctx context.Context
	wmu sync.Mutex
}

func (w *wsConn) Read(v *wire.ClientMsg) error {
	_, data, err := w.c.Read(w.ctx)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func (w *wsConn) Write(v *wire.ServerMsg) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	w.wmu.Lock()
	defer w.wmu.Unlock()
	return w.c.Write(w.ctx, websocket.MessageText, b)
}

func (w *wsConn) Close() error { return w.c.CloseNow() }

func (s *Server) serveHTTP(l net.Listener) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/api/sources", s.handleAPISources)
	mux.HandleFunc("/api/profiles", s.handleAPIProfiles)
	mux.HandleFunc("/api/config", s.handleAPIConfig)
	mux.HandleFunc("/api/docker/containers", s.handleAPIDockerContainers)
	mux.HandleFunc("/api/export", s.handleAPIExport)
	mux.HandleFunc("/api/health", s.handleAPIHealth)
	if s.opts.StaticFS != nil {
		mux.Handle("/", s.opts.StaticFS)
	} else {
		mux.HandleFunc("/", placeholder)
	}
	srv := &http.Server{Handler: mux}
	_ = srv.Serve(l)
}

func (s *Server) handleAPIProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		// Snapshot under the lock; encode without it so a slow client
		// doesn't block save/delete writers.
		s.profilesMu.Lock()
		snap := make([]ProfileInfo, len(s.opts.Profiles))
		copy(snap, s.opts.Profiles)
		s.profilesMu.Unlock()
		if len(snap) == 0 {
			_, _ = w.Write([]byte("[]"))
			return
		}
		_ = json.NewEncoder(w).Encode(snap)
	case http.MethodPost:
		s.handleProfileSave(w, r)
	case http.MethodDelete:
		s.handleProfileDelete(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type saveProfileReq struct {
	Name            string   `json:"name"`
	Filter          string   `json:"filter"`
	Columns         []string `json:"columns"`
	CollapsedFields []string `json:"collapsed_fields"`
	Destination     string   `json:"destination"` // "user" | "repo"
}

func (s *Server) handleProfileSave(w http.ResponseWriter, r *http.Request) {
	var req saveProfileReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	dest := config.SaveDestUser
	if req.Destination == "repo" {
		dest = config.SaveDestRepo
	}
	prof := config.Profile{
		Name:            req.Name,
		Filter:          req.Filter,
		Columns:         req.Columns,
		CollapsedFields: req.CollapsedFields,
	}
	path, err := config.SaveProfile(prof, dest, s.opts.RepoRoot)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.refreshProfiles(req.Name, prof)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "path": path})
}

func (s *Server) handleProfileDelete(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "name query param required", http.StatusBadRequest)
		return
	}
	dest := config.SaveDestUser
	if r.URL.Query().Get("destination") == "repo" {
		dest = config.SaveDestRepo
	}
	path, err := config.DeleteProfile(name, dest, s.opts.RepoRoot)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.removeProfile(name)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "path": path})
}

// refreshProfiles updates s.opts.Profiles in place so subsequent GETs reflect
// the saved profile. Replaces by name if present, else appends.
func (s *Server) refreshProfiles(name string, p config.Profile) {
	info := ProfileInfo{Name: p.Name, Filter: p.Filter, Columns: p.Columns}
	s.profilesMu.Lock()
	defer s.profilesMu.Unlock()
	for i := range s.opts.Profiles {
		if s.opts.Profiles[i].Name == name {
			s.opts.Profiles[i] = info
			return
		}
	}
	s.opts.Profiles = append(s.opts.Profiles, info)
}
func (s *Server) removeProfile(name string) {
	s.profilesMu.Lock()
	defer s.profilesMu.Unlock()
	// Build into a fresh slice so concurrent readers (already holding a
	// snapshot they took under the lock) don't see torn intermediate
	// states. Allocates one slice per delete; not a hot path.
	out := make([]ProfileInfo, 0, len(s.opts.Profiles))
	for _, p := range s.opts.Profiles {
		if p.Name != name {
			out = append(out, p)
		}
	}
	s.opts.Profiles = out
}

func (s *Server) handleAPIConfig(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"theme":           s.opts.Theme,
		"default_profile": s.opts.DefaultProfile,
	})
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		return
	}
	defer c.CloseNow()
	conn := &wsConn{c: c, ctx: r.Context()}
	s.runSession(r.Context(), conn)
}

func (s *Server) handleAPISources(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.Sources())
}

func (s *Server) handleAPIHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	srcs := s.Sources()
	open := 0
	for _, sr := range srcs {
		if sr.State == "open" {
			open++
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"head":         s.store.Head(),
		"tail":         s.store.Tail(),
		"rows":         s.store.Head() - s.store.Tail(),
		"sources":      len(srcs),
		"sources_open": open,
		"sessions":     s.sessionCnt.Load(),
		"started_unix": s.startedAt.Unix(),
	})
}

// dockerContainersCache holds the last successful list for ~2s to absorb
// repeated picker opens without thrashing the docker daemon.
type dockerContainersCache struct {
	at    time.Time
	items []docker.ContainerInfo
	err   string
}

var dockerCache atomic.Pointer[dockerContainersCache]

func (s *Server) handleAPIDockerContainers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if c := dockerCache.Load(); c != nil && time.Since(c.at) < 2*time.Second {
		_ = json.NewEncoder(w).Encode(map[string]any{"containers": c.items, "error": c.err})
		return
	}
	items, err := docker.ListContainers(r.Context())
	entry := &dockerContainersCache{at: time.Now(), items: items}
	if err != nil {
		entry.err = err.Error()
	}
	dockerCache.Store(entry)
	_ = json.NewEncoder(w).Encode(map[string]any{"containers": entry.items, "error": entry.err})
}

const placeholderHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>loggi</title>
<style>
body{font-family:system-ui,sans-serif;background:#0e0f12;color:#e3e6ed;margin:2em;}
code{background:#1a1d24;padding:2px 6px;border-radius:3px;}
a{color:#7aa9ff;}
</style></head>
<body>
<h1>loggi</h1>
<p>Server is running. The Svelte UI bundle hasn't been built yet.</p>
<p>Build it with <code>cd web && pnpm install && pnpm run build</code> and then restart loggi.</p>
<p>WebSocket endpoint: <code>/ws</code></p>
<p>Sources API: <a href="/api/sources">/api/sources</a></p>
</body></html>`

func placeholder(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(placeholderHTML))
}
