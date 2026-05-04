package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"

	"github.com/coder/websocket"
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
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	if s.opts.StaticFS != nil {
		mux.Handle("/", s.opts.StaticFS)
	} else {
		mux.HandleFunc("/", placeholder)
	}
	srv := &http.Server{Handler: mux}
	_ = srv.Serve(l)
}

func (s *Server) handleAPIProfiles(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.opts.Profiles == nil {
		_, _ = w.Write([]byte("[]"))
		return
	}
	_ = json.NewEncoder(w).Encode(s.opts.Profiles)
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
