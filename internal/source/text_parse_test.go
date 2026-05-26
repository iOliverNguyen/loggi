package source

import (
	"math"
	"strings"
	"testing"
	"time"
)

func mustRFC3339(t *testing.T, s string) float64 {
	t.Helper()
	tt, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatalf("setup parse %q: %v", s, err)
	}
	return float64(tt.UnixNano()) / 1e9
}

func mustSpaceLocal(t *testing.T, s string) float64 {
	t.Helper()
	tt, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local)
	if err != nil {
		t.Fatalf("setup parse %q: %v", s, err)
	}
	return float64(tt.UnixNano()) / 1e9
}

func mustTodayLocal(h, m, sec, nsec int) float64 {
	now := time.Now()
	tt := time.Date(now.Year(), now.Month(), now.Day(), h, m, sec, nsec, time.Local)
	return float64(tt.UnixNano()) / 1e9
}

// 1µs tolerance — float64 unix-second representation loses sub-µs
// precision somewhere around 2030, so we don't insist on exact equality.
const tsEpsilon = 1e-6

func TestParseTextLine_UserSamples(t *testing.T) {
	cases := []struct {
		name    string
		line    string
		wantLvl string
		wantTs  float64
	}{
		{
			name:    "kafka_bare_info_time_only",
			line:    `kafka 03:56:52.77 INFO  ==> Subscribe to project updates by watching https://github.com/bitnami/containers`,
			wantLvl: "info",
			wantTs:  mustTodayLocal(3, 56, 52, 770_000_000),
		},
		{
			name:    "bracketed_datetime_bare_info",
			line:    `[2026-05-26 03:57:00,918] INFO [LogLoader partition=__consumer_offsets-19, dir=/bitnami/kafka/data] Loading producer state till offset 0`,
			wantLvl: "info",
			wantTs:  mustSpaceLocal(t, "2026-05-26 03:57:00,918"),
		},
		{
			name:    "datetime_bracketed_debug",
			line:    `2026-05-26 03:56:59.209 [debug] <0.287.0> Lager installed handler error_logger_lager_h into error_logger`,
			wantLvl: "debug",
			wantTs:  mustSpaceLocal(t, "2026-05-26 03:56:59.209"),
		},
		{
			name:    "rfc3339_bracketed_info",
			line:    `2026-05-26T04:16:50.051Z [info] [AMQP]: Created AMQP queue: amq.gen-h9LiarNZr7_JCDftkQVs5w {"service":"chat","env":"local","version":"unspecified"}`,
			wantLvl: "info",
			wantTs:  mustRFC3339(t, "2026-05-26T04:16:50.051Z"),
		},
		{
			name:    "rfc3339_micro_bracketed_warning",
			line:    `2026-05-26T04:17:13.300406Z [warning  ] [FF] flag evaluation failed, no stale cache available [sdks.feature_flag.client]`,
			wantLvl: "warn",
			wantTs:  mustRFC3339(t, "2026-05-26T04:17:13.300406Z"),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotTs, gotLvl := ParseTextLine(c.line)
			if gotLvl != c.wantLvl {
				t.Errorf("level = %q, want %q", gotLvl, c.wantLvl)
			}
			if math.Abs(gotTs-c.wantTs) > tsEpsilon {
				t.Errorf("ts = %.9f, want %.9f (diff %.9f)", gotTs, c.wantTs, gotTs-c.wantTs)
			}
		})
	}
}

func TestParseTextLine_AliasesViaBracket(t *testing.T) {
	// Bracketed path goes through NormalizeLevel, so every alias the
	// JSON path supports should resolve here too.
	cases := map[string]string{
		`prefix [trace] msg`:    "trace",
		`prefix [verbose] msg`:  "trace",
		`prefix [notice] msg`:   "notice",
		`prefix [warning] msg`:  "warn",
		`prefix [err] msg`:      "error",
		`prefix [severe] msg`:   "error",
		`prefix [critical] msg`: "fatal",
		`prefix [panic] msg`:    "fatal",
		`prefix [Info] msg`:     "info",
		`prefix [DEBUG] msg`:    "debug",
		`prefix [warning  ] msg`: "warn", // padded
	}
	for line, want := range cases {
		t.Run(line, func(t *testing.T) {
			if _, got := ParseTextLine(line); got != want {
				t.Errorf("level = %q, want %q", got, want)
			}
		})
	}
}

func TestParseTextLine_Negatives(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"no signals", `Hello world this is just a regular line`},
		{"lowercase info mid-message", `random output mentioning info as a word`},
		{"non-time colons", `address ::1 received connection from peer`},
		{"json object line", `{"level":"info","msg":"ok"}`}, // upstream should classify as JSON; we still must not crash
		{"empty", ``},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ts, lvl := ParseTextLine(c.line)
			if ts != 0 {
				t.Errorf("ts = %v, want 0", ts)
			}
			if lvl != "" {
				t.Errorf("level = %q, want empty", lvl)
			}
		})
	}
}

func TestParseTextLine_BareUppercaseAliases(t *testing.T) {
	// Bare-word path requires uppercase + length>=3. Aliases beyond the
	// original canonical six (WARN/INFO/...) should still resolve.
	cases := map[string]string{
		`2026-05-26T04:16:50Z WARNING something happened`: "warn",
		`2026-05-26T04:16:50Z SEVERE crash`:               "error",
		`2026-05-26T04:16:50Z CRITICAL alert`:             "fatal",
	}
	for line, want := range cases {
		t.Run(line, func(t *testing.T) {
			if _, got := ParseTextLine(line); got != want {
				t.Errorf("level = %q, want %q", got, want)
			}
		})
	}
}

func TestParseTextLine_LevelOnlyOutOfScanWindow(t *testing.T) {
	// The level word sits past textScanLimit — should not be picked up.
	line := strings.Repeat("x", textScanLimit+10) + " ERROR boom"
	if _, lvl := ParseTextLine(line); lvl != "" {
		t.Errorf("expected no level past scan window, got %q", lvl)
	}
}

func TestParseTextLine_RFC3339NumericOffset(t *testing.T) {
	// +0700 (no colon) variant — common in some loggers.
	line := `2026-05-26T04:16:50+0700 INFO startup`
	want := mustRFC3339(t, "2026-05-26T04:16:50+07:00")
	gotTs, gotLvl := ParseTextLine(line)
	if gotLvl != "info" {
		t.Errorf("level = %q, want info", gotLvl)
	}
	if math.Abs(gotTs-want) > tsEpsilon {
		t.Errorf("ts = %.9f, want %.9f", gotTs, want)
	}
}
