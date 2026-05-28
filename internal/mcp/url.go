package mcp

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// SessionConfig mirrors the v=1 shape encoded by
// web/src/lib/session-url.ts encodeSession. The hash fragment readSessionFromHash
// produces is base64url(JSON of this struct, with empty fields elided).
type SessionConfig struct {
	Filter  string   `json:"filter,omitempty"`
	Profile string   `json:"profile,omitempty"`
	Columns []string `json:"columns,omitempty"`
	Paused  bool     `json:"paused,omitempty"`
}

// buildViewURL returns "<httpURL>/#s=<base64url(json)>". Matches the format
// readSessionFromHash decodes in the web client.
func buildViewURL(httpURL string, cfg SessionConfig) (string, error) {
	if httpURL == "" {
		return "", errors.New("daemon HTTP URL is not set yet")
	}
	enc, err := encodeSessionConfig(cfg)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(httpURL, "/") + "/#s=" + enc, nil
}

// encodeSessionConfig serializes cfg the same way the web UI does: a v=1
// JSON object with empty fields elided, then base64url without padding.
func encodeSessionConfig(cfg SessionConfig) (string, error) {
	// Build the slim object manually so field order matches encodeSession
	// closely enough to be debuggable; the decoder doesn't care about order.
	slim := map[string]any{"v": 1}
	if cfg.Filter != "" {
		slim["filter"] = cfg.Filter
	}
	if cfg.Profile != "" {
		slim["profile"] = cfg.Profile
	}
	if cfg.Paused {
		slim["paused"] = true
	}
	if len(cfg.Columns) > 0 {
		slim["columns"] = cfg.Columns
	}
	b, err := json.Marshal(slim)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
