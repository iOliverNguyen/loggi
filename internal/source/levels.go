package source

import (
	"encoding/json"
	"strings"
)

// Canonical level vocabulary written to the store after normalization.
// Filter expressions and the UI both index against this set, so adding a
// new value has UI repercussions — keep this short.
const (
	LevelTrace  = "trace"
	LevelDebug  = "debug"
	LevelInfo   = "info"
	LevelNotice = "notice"
	LevelWarn   = "warn"
	LevelError  = "error"
	LevelFatal  = "fatal"
)

// stringLevels maps every spelling we've seen in the wild to the canonical
// form. Keys are pre-lowercased and trimmed; lookup builds an index off
// this map at init.
var stringLevels = map[string]string{
	// trace family
	"trace": LevelTrace, "verbose": LevelTrace,
	"finest": LevelTrace, "finer": LevelTrace,
	"t": LevelTrace,

	// debug family
	"debug": LevelDebug, "debg": LevelDebug, // rust2 ships "DEBG"
	"fine": LevelDebug, // java.util.logging
	"d":    LevelDebug,

	// info family
	"info": LevelInfo, "information": LevelInfo, "informational": LevelInfo,
	"i": LevelInfo,

	// notice
	"notice": LevelNotice,

	// warn family
	"warn": LevelWarn, "warning": LevelWarn,
	"w": LevelWarn,

	// error family
	"error": LevelError, "err": LevelError,
	"erro":   LevelError, // rust2 ships "ERRO"
	"severe": LevelError, // java.util.logging
	"e":      LevelError,

	// fatal family — anything more urgent than error collapses here, since
	// the UI doesn't have a separate row colour for alert/emerg.
	"fatal": LevelFatal, "critical": LevelFatal, "crit": LevelFatal,
	"panic":     LevelFatal,
	"alert":     LevelFatal,
	"emerg":     LevelFatal,
	"emergency": LevelFatal,
	"f":         LevelFatal,
}

// NormalizeLevel maps a raw string level value to the canonical lowercase
// form. Empty input returns empty; unknown words return empty so the
// caller can fall through to the raw value rather than mis-classify.
func NormalizeLevel(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	return stringLevels[s]
}

// NormalizeLevelAny accepts the polymorphic level shapes seen in real
// loggers:
//
//   - string  → NormalizeLevel
//   - number  → numeric scheme dispatch by magnitude
//   - object  → look for a ".name" string property and recurse
//
// Numeric magnitudes overlap (e.g. 4 could be SemanticLogger:error or
// syslog:warn), so a sibling string-level field — when present in the
// containing object — usually wins via extractLevel before this is
// reached. As a standalone classifier we pick the most common scheme
// per range.
func NormalizeLevelAny(v any) string {
	switch x := v.(type) {
	case string:
		return NormalizeLevel(x)
	case float64:
		return normalizeNumericLevel(x)
	case int:
		return normalizeNumericLevel(float64(x))
	case int64:
		return normalizeNumericLevel(float64(x))
	case map[string]any:
		if name, ok := x["name"].(string); ok {
			return NormalizeLevel(name)
		}
		// loguru also exposes level.no (number) on the same object.
		if no, ok := x["no"]; ok {
			return NormalizeLevelAny(no)
		}
	case json.Number:
		if f, err := x.Float64(); err == nil {
			return normalizeNumericLevel(f)
		}
	}
	return ""
}

// normalizeNumericLevel dispatches by magnitude to the dominant scheme for
// that range. Schemes seen in the corpus:
//
//   - 0–7         syslog severity (RFC 5424)
//   - 10–60       Bunyan / pino
//   - 100–700     Monolog (PSR-3-ish numbering)
//   - ≥1000       Logback `level_value`
func normalizeNumericLevel(f float64) string {
	n := int(f)
	switch {
	case n >= 1000:
		return logbackLevel(n)
	case n >= 100:
		return monologLevel(n)
	case n >= 10:
		return bunyanLevel(n)
	case n >= 0 && n <= 7:
		return syslogLevel(n)
	}
	return ""
}

func bunyanLevel(n int) string {
	switch {
	case n < 20:
		return LevelTrace
	case n < 30:
		return LevelDebug
	case n < 40:
		return LevelInfo
	case n < 50:
		return LevelWarn
	case n < 60:
		return LevelError
	default:
		return LevelFatal
	}
}

func monologLevel(n int) string {
	switch {
	case n < 200:
		return LevelDebug
	case n < 250:
		return LevelInfo
	case n < 300:
		return LevelNotice
	case n < 400:
		return LevelWarn
	case n < 500:
		return LevelError
	default:
		return LevelFatal
	}
}

func logbackLevel(n int) string {
	switch {
	case n < 10000:
		return LevelTrace
	case n < 20000:
		return LevelDebug
	case n < 30000:
		return LevelInfo
	case n < 40000:
		return LevelWarn
	default:
		return LevelError
	}
}

// syslog severity: lower number = more severe.
func syslogLevel(n int) string {
	switch n {
	case 0, 1, 2:
		return LevelFatal
	case 3:
		return LevelError
	case 4:
		return LevelWarn
	case 5:
		return LevelNotice
	case 6:
		return LevelInfo
	case 7:
		return LevelDebug
	}
	return ""
}

// ExtractLevel finds the best canonical level signal in a JSON object,
// walking source.AliasMap["level"] in order (which includes dotted paths
// for nested envelopes like loguru's `record.level.name`). Returns the
// empty string when nothing classifiable was found — callers should
// leave the level slot untouched in that case rather than overwriting
// with "".
//
// String forms beat numeric: when both `level` (numeric) and `level_name`
// (string) are present (Monolog), the string wins by virtue of yielding
// a non-empty NormalizeLevel result on the first match.
func ExtractLevel(obj map[string]json.RawMessage) string {
	for _, member := range AliasMap["level"] {
		raw, ok := lookupRaw(obj, member)
		if !ok || len(raw) == 0 {
			continue
		}
		if v := normalizeRaw(raw); v != "" {
			return v
		}
	}
	return ""
}

// lookupRaw returns the json.RawMessage at the given dotted path within
// obj. Tries a literal-key match first (ECS keys like "log.level" live
// flat in the JSON despite the dot) before splitting and walking.
func lookupRaw(obj map[string]json.RawMessage, path string) (json.RawMessage, bool) {
	if raw, ok := obj[path]; ok {
		return raw, true
	}
	if !strings.Contains(path, ".") {
		return nil, false
	}
	parts := strings.Split(path, ".")
	// Top of path: must be a key in obj. From there each segment walks
	// the embedded JSON object.
	first := parts[0]
	cur, ok := obj[first]
	if !ok {
		return nil, false
	}
	for _, seg := range parts[1:] {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(cur, &m); err != nil {
			return nil, false
		}
		next, ok := m[seg]
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

// normalizeRaw decodes a json.RawMessage into the appropriate primitive
// and runs it through NormalizeLevelAny. Object payloads (loguru's
// `level: {name: "DEBUG"}`) are decoded into map[string]any so the
// recursive .name lookup works.
func normalizeRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	switch raw[0] {
	case '"':
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return NormalizeLevel(s)
		}
	case '{':
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err == nil {
			return NormalizeLevelAny(m)
		}
	default:
		// number / true / false / null — try number.
		var f float64
		if err := json.Unmarshal(raw, &f); err == nil {
			return normalizeNumericLevel(f)
		}
	}
	return ""
}
