package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/iOliverNguyen/loggi/internal/config"
)

// Health is the subset of /api/health fields used for daemon discovery.
// The endpoint returns more fields (head, rows, sessions, etc.); they're
// ignored here so the schema can grow without breaking discovery.
type Health struct {
	PID         int    `json:"pid"`
	Socket      string `json:"socket"`
	StartedUnix int64  `json:"started_unix"`
	Rows        uint64 `json:"rows"`
	Sources     int    `json:"sources"`
	SourcesOpen int    `json:"sources_open"`
	Sessions    int64  `json:"sessions"`
}

// DiscoverViaHealth probes <url>/api/health and returns the daemon's
// self-reported state. A 500ms timeout keeps this cheap on the dial
// path — a running local daemon answers in microseconds, and a silent
// port should not block the caller.
//
// "Is this loggi?" is decided on started_unix, which every loggi build
// has exposed since /api/health was introduced. The newer pid/socket
// fields may be absent from older daemons; callers must handle PID==0
// and Socket=="" gracefully (status prints a hint to restart; stop
// refuses to signal an unknown pid).
func DiscoverViaHealth(url string) (*Health, error) {
	if url == "" {
		return nil, errors.New("empty URL")
	}
	c := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := c.Get(strings.TrimRight(url, "/") + "/api/health")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health HTTP %d", resp.StatusCode)
	}
	var h Health
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return nil, err
	}
	if h.StartedUnix == 0 {
		return nil, errors.New("health response missing started_unix — not a loggi server?")
	}
	return &h, nil
}

// DiscoverRunningDaemon tries runtime.json first (authoritative when
// present), then falls back to the configured http_bind. Returns the
// first health response it can get, or (nil, nil) if no loggi is
// reachable at either location. Errors are not propagated up — this is
// a best-effort discovery helper for Dial/status/stop.
func DiscoverRunningDaemon() (*Health, string) {
	if info, err := ReadRuntime(); err == nil && info.HTTP != "" {
		if h, err := DiscoverViaHealth(info.HTTP); err == nil {
			return h, info.HTTP
		}
	}
	url := configBindURL()
	if url == "" {
		return nil, ""
	}
	if h, err := DiscoverViaHealth(url); err == nil {
		return h, url
	}
	return nil, ""
}

// configBindURL returns the HTTP URL derived from the merged config's
// server.http_bind, or "" if config can't be loaded. Used as the
// fallback when runtime.json is absent.
func configBindURL() string {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	loaded, err := config.Load(cwd)
	if err != nil {
		return ""
	}
	return HTTPBindURL(loaded.Config.Server.HTTPBind)
}

// defaultHTTPBind mirrors server.DefaultHTTPBind. It's redefined here
// rather than imported so the client package doesn't drag in the
// server package; keep the two values in sync.
const defaultHTTPBind = "127.0.0.1:9199"

// HTTPBindURL turns a "host:port" bind address into an "http://host:port"
// URL, normalising a wildcard or empty host to a loopback address. An
// empty bind maps to the daemon's default bind (so discovery probes the
// same address the daemon actually listens on when the user's config
// leaves http_bind unset).
func HTTPBindURL(bind string) string {
	if bind == "" {
		bind = defaultHTTPBind
	}
	host, port, err := net.SplitHostPort(bind)
	if err != nil {
		return "http://" + bind
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}
