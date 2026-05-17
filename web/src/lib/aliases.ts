// Cross-language synonym map mirrored from internal/source/aliases.go.
// The canonical id is the key; the value lists raw JSON field names the
// renderer should walk in order until it finds a non-empty value.
//
// Render-only: raw keys stay raw in entry.fields. Filter expressions
// continue to match against the actual key (`timestamp:>2026` still works).
// The alias chain is consulted only when a column wants the "logical"
// view — a Python `timestamp` row renders inside the same Time column as
// a Go `ts` row, even though the underlying keys differ.
//
// Keep in sync with internal/source/aliases.go. Adding a logical id here
// without the Go side won't break anything (the sampler simply emits a
// raw `@key` instead of the logical id), but the reverse — adding a
// logical id on the Go side without mirroring here — causes the column
// renderer to receive an unknown id and show "—".
export const ALIAS_MAP: Record<string, string[]> = {
  ts: [
    "ts", "timestamp", "@timestamp",
    "time", "datetime",
    "fields.timestamp",       // tracing-subscriber (rare)
    "record.time.timestamp",  // loguru
  ],
  msg: [
    "msg", "message",
    "event",          // structlog
    "fields.message", // tracing-subscriber
    "record.message", // loguru
  ],
  caller: [
    "caller", "callerFunc",
    "logger", "logger_name", // python stdlib, logback
    "log.logger",                            // ECS
    "target", "module_path", "module",       // rust tracing / log4rs / loguru
    "channel",                               // monolog
    "progname",                              // ruby logger
    "record.function", "record.file.name",   // loguru
  ],
  service: [
    "service",
    "application",          // ruby SemanticLogger
    "span.service",          // rust tracing-subscriber
    "mdc.service",           // rust log4rs / logback MDC
    "record.extra.service",  // loguru
  ],
  level: [
    "level",
    "level_name",        // monolog string form
    "log.level",         // ECS
    "monolog_level",     // php2 numeric
    "level_value",       // logback numeric
    "level_index",       // ruby SemanticLogger numeric
    "severity",          // GCP / syslog
    "level.name",        // defensive shallow nested
    "record.level.name", // loguru
    "record.level",      // loguru bare string
  ],
};

// Priorities is the fixed display order for canonical columns when
// merging a recommendation into the column list. Mirrors
// source.Priorities on the Go side.
export const PRIORITIES = ["ts", "level", "msg", "service", "caller"];

const rawToLogical: Record<string, string> = (() => {
  const m: Record<string, string> = {};
  for (const [logical, members] of Object.entries(ALIAS_MAP)) {
    for (const raw of members) {
      m[raw] = logical;
    }
  }
  return m;
})();

// resolveAlias returns the canonical logical id for a raw key, or the raw
// key itself if it's not part of any alias group. Used by FacetPanel to
// collapse "timestamp" rows under the same chip as "ts" rows.
export function resolveAlias(rawKey: string): string {
  return rawToLogical[rawKey] ?? rawKey;
}

// isLogicalId returns true if id is a canonical alias-group key.
export function isLogicalId(id: string): boolean {
  return id in ALIAS_MAP;
}

// aliasMembers returns the ordered fallback chain for a logical id, or
// [id] if it isn't a known alias group.
export function aliasMembers(id: string): string[] {
  return ALIAS_MAP[id] ?? [id];
}
