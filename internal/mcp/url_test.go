package mcp

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// decodeBackToConfig mimics the inverse of encodeSession in
// web/src/lib/session-url.ts so we can round-trip in tests without spinning
// up the SPA.
func decodeBackToConfig(t *testing.T, fragment string) map[string]any {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(fragment)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	return out
}

func TestBuildViewURL_RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		cfg  SessionConfig
		want map[string]any
	}{
		{
			name: "empty cfg encodes just v=1",
			cfg:  SessionConfig{},
			want: map[string]any{"v": float64(1)},
		},
		{
			name: "filter only",
			cfg:  SessionConfig{Filter: "level:error"},
			want: map[string]any{"v": float64(1), "filter": "level:error"},
		},
		{
			name: "all fields",
			cfg: SessionConfig{
				Filter:  "service:api level:>=warn",
				Profile: "api",
				Columns: []string{"ts", "level", "msg", "trace_id"},
				Paused:  true,
			},
			want: map[string]any{
				"v":       float64(1),
				"filter":  "service:api level:>=warn",
				"profile": "api",
				"columns": []any{"ts", "level", "msg", "trace_id"},
				"paused":  true,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url, err := buildViewURL("http://127.0.0.1:9199", tc.cfg)
			if err != nil {
				t.Fatalf("buildViewURL: %v", err)
			}
			const prefix = "http://127.0.0.1:9199/#s="
			if !strings.HasPrefix(url, prefix) {
				t.Fatalf("url %q missing prefix %q", url, prefix)
			}
			got := decodeBackToConfig(t, strings.TrimPrefix(url, prefix))
			if !mapsEqual(got, tc.want) {
				t.Fatalf("decoded mismatch\n got: %#v\nwant: %#v", got, tc.want)
			}
		})
	}
}

func TestBuildViewURL_EmptyHTTPURL(t *testing.T) {
	if _, err := buildViewURL("", SessionConfig{}); err == nil {
		t.Fatal("expected error for empty httpURL")
	}
}

func TestBuildViewURL_TrailingSlash(t *testing.T) {
	url, err := buildViewURL("http://127.0.0.1:9199/", SessionConfig{Filter: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(url, "//#s=") {
		t.Fatalf("trailing slash not normalized: %q", url)
	}
}

func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok {
			return false
		}
		if !valEqual(va, vb) {
			return false
		}
	}
	return true
}

func valEqual(a, b any) bool {
	switch av := a.(type) {
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !valEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}
