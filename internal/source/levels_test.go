package source

import (
	"encoding/json"
	"testing"
)

func TestNormalizeLevel_Strings(t *testing.T) {
	cases := map[string]string{
		// Mixed-case variants from the fixture catalogue.
		"INFO": LevelInfo, "info": LevelInfo, "Info": LevelInfo,
		"DEBUG": LevelDebug, "debug": LevelDebug,
		"DEBG":  LevelDebug, // rust2 typo
		"FINE":  LevelDebug, // java.util.logging
		"WARN":  LevelWarn, "warn": LevelWarn,
		"WARNING": LevelWarn, "warning": LevelWarn,
		"ERROR": LevelError, "error": LevelError,
		"ERR":  LevelError, "err": LevelError,
		"ERRO":   LevelError, // rust2 typo
		"SEVERE": LevelError, // java.util.logging
		"FATAL":  LevelFatal, "fatal": LevelFatal,
		"critical": LevelFatal, "CRITICAL": LevelFatal,
		"panic": LevelFatal,
		"trace": LevelTrace, "TRACE": LevelTrace,
		"verbose": LevelTrace,
		"notice":  LevelNotice,

		// Whitespace / unknowns
		"  INFO  ": LevelInfo,
		"":          "",
		"random":    "",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			if got := NormalizeLevel(in); got != want {
				t.Errorf("NormalizeLevel(%q) = %q, want %q", in, got, want)
			}
		})
	}
}

func TestNormalizeLevelAny_Numeric(t *testing.T) {
	cases := []struct {
		in   float64
		want string
		name string
	}{
		// Bunyan / pino
		{10, LevelTrace, "bunyan-trace"},
		{20, LevelDebug, "bunyan-debug"},
		{30, LevelInfo, "bunyan-info"},
		{40, LevelWarn, "bunyan-warn"},
		{50, LevelError, "bunyan-error"},
		{60, LevelFatal, "bunyan-fatal"},

		// Monolog
		{100, LevelDebug, "monolog-debug"},
		{200, LevelInfo, "monolog-info"},
		{250, LevelNotice, "monolog-notice"},
		{300, LevelWarn, "monolog-warn"},
		{400, LevelError, "monolog-error"},
		{500, LevelFatal, "monolog-fatal-critical"},
		{550, LevelFatal, "monolog-fatal-alert"},
		{600, LevelFatal, "monolog-fatal-emergency"},

		// Logback level_value
		{5000, LevelTrace, "logback-trace"},
		{10000, LevelDebug, "logback-debug"},
		{20000, LevelInfo, "logback-info"},
		{30000, LevelWarn, "logback-warn"},
		{40000, LevelError, "logback-error"},

		// Syslog severity
		{0, LevelFatal, "syslog-emerg"},
		{3, LevelError, "syslog-err"},
		{4, LevelWarn, "syslog-warn"},
		{6, LevelInfo, "syslog-info"},
		{7, LevelDebug, "syslog-debug"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NormalizeLevelAny(c.in); got != c.want {
				t.Errorf("NormalizeLevelAny(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestNormalizeLevelAny_Loguru exercises loguru's `level: {name, no, icon}`
// object form. The .name string takes precedence; if absent, the .no
// number is dispatched through the numeric scheme.
func TestNormalizeLevelAny_Loguru(t *testing.T) {
	withName := map[string]any{"icon": "🐞", "name": "DEBUG", "no": 10}
	if got := NormalizeLevelAny(withName); got != LevelDebug {
		t.Errorf("loguru obj with .name=DEBUG: got %q want %q", got, LevelDebug)
	}
	onlyNo := map[string]any{"no": 30.0}
	if got := NormalizeLevelAny(onlyNo); got != LevelInfo {
		t.Errorf("loguru obj with only .no=30 (bunyan): got %q want %q", got, LevelInfo)
	}
}

// TestExtractLevel drives the full walker against representative shapes
// from __/cases. We synthesize one JSON line per shape rather than
// reading the fixture directly — the goal is to verify the walker
// finds the right key, not to fully cover the corpus.
func TestExtractLevel(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "go-string", in: `{"level":"info","msg":"x"}`, want: LevelInfo},
		{name: "go-uppercase", in: `{"level":"INFO","msg":"x"}`, want: LevelInfo},
		{name: "java-severe", in: `{"level":"SEVERE","msg":"x"}`, want: LevelError},
		{name: "rust-typo", in: `{"level":"DEBG","msg":"x"}`, want: LevelDebug},
		{name: "node-pino-numeric", in: `{"level":30,"msg":"x"}`, want: LevelInfo},
		{name: "monolog-numeric", in: `{"level":300,"msg":"x"}`, want: LevelWarn},
		{name: "logback-pair", in: `{"level":"WARN","level_value":30000}`, want: LevelWarn},
		{name: "monolog-pair", in: `{"level":400,"level_name":"ERROR"}`, want: LevelError},
		{name: "ecs-log-level", in: `{"log.level":"INFO","message":"x"}`, want: LevelInfo},
		{name: "loguru-record", in: `{"text":"x","record":{"level":{"name":"DEBUG","no":10}}}`, want: LevelDebug},
		{name: "loguru-record-string", in: `{"record":{"level":"WARNING"}}`, want: LevelWarn},
		{name: "unknown", in: `{"foo":"bar"}`, want: ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal([]byte(c.in), &obj); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := ExtractLevel(obj); got != c.want {
				t.Errorf("ExtractLevel = %q want %q", got, c.want)
			}
		})
	}
}
