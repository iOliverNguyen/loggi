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
// self-reported pid + socket. A 500ms timeout keeps this cheap on the
// dial path — a running local daemon answers in microseconds, and a
// silent port should not block the caller. Returns an error if the URL
// is unreachable, returns non-200, or the response doesn't carry the
// fields we need (i.e. it isn't a loggi server).
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
	if h.PID == 0 || h.Socket == "" {
		return nil, errors.New("health response missing pid/socket — not a loggi server?")
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

// HTTPBindURL turns a "host:port" bind address into an "http://host:port"
// URL, normalising a wildcard or empty host to a loopback address. Empty
// input returns "" so callers can distinguish "not configured" from a
// usable URL.
func HTTPBindURL(bind string) string {
	if bind == "" {
		return ""
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
