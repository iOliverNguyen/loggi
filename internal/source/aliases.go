package source

import "slices"

// AliasMap unifies cross-language synonyms onto a single logical column id.
// The key is the canonical id (matches an existing UI builtin where one
// exists); the value lists raw JSON field names to walk in order at render
// time.
//
// Render-only: raw keys are NOT rewritten at ingest. Filter expressions
// keep working against the actual key in the entry (e.g. `timestamp:>2026`
// still finds Python rows). The alias chain only affects column rendering
// and detection: a sampler counting "timestamp" hits picks the canonical
// id "ts" so a single time column suffices for Go, Node, and Python rows.
//
// Keep this map small. Each entry implies UI compatibility down the whole
// renderer chain (frontend mirror in web/src/lib/aliases.ts, the builtin
// ts/msg/caller renderers in LogRow.svelte, FacetPanel chip resolution).
var AliasMap = map[string][]string{
	"ts": {
		"ts", "timestamp", "@timestamp",
		"time", "datetime",
		"fields.timestamp",       // tracing-subscriber (rare)
		"record.time.timestamp",  // loguru
	},
	"msg": {
		"msg", "message",
		"event",             // structlog
		"fields.message",    // tracing-subscriber
		"record.message",    // loguru
	},
	"caller": {
		"caller", "callerFunc",
		"logger", "logger_name", // python stdlib, logback
		"log.logger",                          // ECS
		"target", "module_path", "module",     // rust tracing / log4rs / loguru
		"channel",                              // monolog
		"progname",                             // ruby logger
		"record.function", "record.file.name", // loguru
	},
	"service": {
		"service",
		"application",         // ruby SemanticLogger
		"span.service",         // rust tracing-subscriber
		"mdc.service",          // rust log4rs / java logback MDC
		"record.extra.service", // loguru extras
	},
	"level": {
		"level",
		"level_name",          // monolog string form
		"log.level",           // ECS
		"monolog_level",       // php2 numeric
		"level_value",         // logback numeric
		"level_index",         // ruby SemanticLogger numeric
		"severity",            // GCP / syslog
		"level.name",          // defensive shallow nested
		"record.level.name",   // loguru
		"record.level",        // loguru when level is a bare string
	},
}

// Priorities lists the canonical field ids that get a free pass through
// the column-detection thresholds. If any of them is observed with at
// least PriorityPresence presence, it is always recommended — even if its
// values are constant (kills the distinct≥2 floor) or unusually long
// (kills the value-length filter). Order is meaningful: canonicals
// surface in this order at the front of the recommendation, ahead of
// score-ranked non-canonicals.
var Priorities = []string{"ts", "level", "msg", "service", "caller"}

// PriorityPresence is the minimum presence ratio a priority field needs
// to qualify for hard-inclusion. Below this the field is too rare to
// reliably surface as a column.
const PriorityPresence = 0.5

// IsPriority returns true if logicalID is in Priorities.
func IsPriority(logicalID string) bool {
	return slices.Contains(Priorities, logicalID)
}

var rawToLogical = func() map[string]string {
	m := make(map[string]string)
	for logical, members := range AliasMap {
		for _, raw := range members {
			m[raw] = logical
		}
	}
	return m
}()

// Resolve returns the canonical logical id for a raw key (e.g. "message" →
// "msg", "timestamp" → "ts"), or "" if the key isn't part of any alias
// group. A key already equal to its canonical id resolves to itself.
func Resolve(rawKey string) string {
	return rawToLogical[rawKey]
}

// Members returns the ordered raw-key fallback chain for a logical id, or
// nil if the id isn't a known alias group.
func Members(logicalID string) []string {
	return AliasMap[logicalID]
}
