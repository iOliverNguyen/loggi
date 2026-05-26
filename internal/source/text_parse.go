package source

import (
	"regexp"
	"time"
)

// ParseTextLine extracts a unix-seconds timestamp and a canonical level
// from a non-JSON log line. Either return may be zero/empty when not
// found. Never modifies the line; callers keep msg verbatim.
func ParseTextLine(line string) (ts float64, level string) {
	return parseTextTimestamp(line), parseTextLevel(line)
}

// textScanLimit bounds how far into the line we look for timestamp and
// level signals. Long enough to clear common prefixes (container name,
// [bracketed timestamp]) but short enough to avoid matching tokens deep
// in the message body — e.g. the word "ERROR" sitting in a stacktrace.
const textScanLimit = 96

var (
	reTextFullTS = regexp.MustCompile(
		`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:?\d{2})?`,
	)
	reTextTimeOnly = regexp.MustCompile(
		`\b\d{2}:\d{2}:\d{2}(?:[.,]\d+)?\b`,
	)
	// Letters-only inside the brackets so a [2026-05-26 ...] timestamp
	// bracket never matches the level slot.
	reTextBracketed = regexp.MustCompile(`\[\s*([A-Za-z]+)\s*\]`)
	reTextWord      = regexp.MustCompile(`[A-Za-z]+`)
)

func parseTextTimestamp(line string) float64 {
	head := line
	if len(head) > textScanLimit {
		head = head[:textScanLimit]
	}
	if m := reTextFullTS.FindString(head); m != "" {
		if t, ok := parseFullTimestamp(m); ok {
			return float64(t.UnixNano()) / 1e9
		}
		// Regex consumed the time portion; falling through to time-only
		// would partial-match what we just rejected.
		return 0
	}
	if m := reTextTimeOnly.FindString(head); m != "" {
		if t, ok := parseTimeOnly(m); ok {
			return float64(t.UnixNano()) / 1e9
		}
	}
	return 0
}

// fullTextLayouts is ordered most-specific first. Layouts ending in
// Z07:00 carry their own zone and use time.Parse; the bare layouts have
// no zone and use ParseInLocation with time.Local so wall-clock-ish
// container output lands on the user's clock rather than UTC.
var fullTextLayouts = []struct {
	layout string
	zoned  bool
}{
	{"2006-01-02T15:04:05Z07:00", true},
	{"2006-01-02T15:04:05Z0700", true},
	{"2006-01-02T15:04:05", false},
	{"2006-01-02 15:04:05Z07:00", true},
	{"2006-01-02 15:04:05Z0700", true},
	{"2006-01-02 15:04:05", false},
}

func parseFullTimestamp(s string) (time.Time, bool) {
	for _, l := range fullTextLayouts {
		var (
			t   time.Time
			err error
		)
		if l.zoned {
			t, err = time.Parse(l.layout, s)
		} else {
			t, err = time.ParseInLocation(l.layout, s, time.Local)
		}
		if err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func parseTimeOnly(s string) (time.Time, bool) {
	t, err := time.ParseInLocation("15:04:05", s, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	now := time.Now()
	return time.Date(
		now.Year(), now.Month(), now.Day(),
		t.Hour(), t.Minute(), t.Second(), t.Nanosecond(),
		time.Local,
	), true
}

func parseTextLevel(line string) string {
	head := line
	if len(head) > textScanLimit {
		head = head[:textScanLimit]
	}
	// Bracketed level wins — an explicit field marker, accept any case.
	for _, m := range reTextBracketed.FindAllStringSubmatch(head, -1) {
		if lvl := NormalizeLevel(m[1]); lvl != "" {
			return lvl
		}
	}
	// Bare token fallback: uppercase, length >= 3. Tighter than
	// NormalizeLevel's full alias set on purpose — the single-letter
	// aliases (`T`, `D`, `I`...) would false-match the `T` in an
	// RFC3339 timestamp, and lowercase English words like "fine" or
	// "info" appear in regular prose.
	for _, tok := range reTextWord.FindAllString(head, -1) {
		if len(tok) < 3 || !isUpperAlpha(tok) {
			continue
		}
		if lvl := NormalizeLevel(tok); lvl != "" {
			return lvl
		}
	}
	return ""
}

func isUpperAlpha(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}
