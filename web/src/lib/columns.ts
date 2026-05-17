// Column configuration for the log list. A `Column` is either a
// built-in (special renderer for ts/level/source/service/msg/caller) or
// a user-added field column that pulls a dotted JSON path out of
// `entry.fields`.
//
// Storage layout:
//   - loggi.columns.v1            — legacy GLOBAL list (still read as baseline)
//   - loggi.columns.bySource.v1   — per-source overrides keyed by "kind:name"
//   - loggi.columns.widths.v1     — per-column pixel widths, shared
//
// Baseline (Profile.Columns server-side) is what the UI shows when no
// source filter is active OR when multiple sources are visible. Per-source
// overrides apply only when the user has scoped the view to one source.

import type { Entry } from "./types";
import { isLogicalId, aliasMembers } from "./aliases";

export type ColumnKind = "builtin" | "field";

export interface Column {
  id: string; // built-in name OR "@dotted.path"
  label: string;
  kind: ColumnKind;
  width: number; // px; 0 = flex (only valid for `msg`)
  visible: boolean;
}

export const BUILTINS: Record<string, { label: string; width: number }> = {
  ts: { label: "time", width: 96 },
  level: { label: "level", width: 56 },
  source: { label: "source", width: 100 },
  service: { label: "service", width: 128 },
  caller: { label: "caller", width: 160 },
  msg: { label: "message", width: 0 },
};

export const DEFAULT_COLUMNS: Column[] = [
  { id: "ts", label: "time", kind: "builtin", width: 96, visible: true },
  { id: "level", label: "level", kind: "builtin", width: 56, visible: true },
  { id: "source", label: "source", kind: "builtin", width: 100, visible: true },
  { id: "service", label: "service", kind: "builtin", width: 128, visible: true },
  { id: "caller", label: "caller", kind: "builtin", width: 160, visible: false },
  { id: "msg", label: "message", kind: "builtin", width: 0, visible: true },
];

const KEY_COLUMNS = "loggi.columns.v1";
const KEY_BY_SOURCE = "loggi.columns.bySource.v1";
const KEY_WIDTHS = "loggi.columns.widths.v1";

// sourceKey is "kind:name" — the stable identity used to key per-source
// column overrides. Mirrors the Go side's persistence key.
export function sourceKey(kind: string, name: string): string {
  return `${kind}:${name}`;
}

export function loadColumns(): Column[] {
  try {
    const raw = localStorage.getItem(KEY_COLUMNS);
    if (!raw) return [...DEFAULT_COLUMNS];
    const parsed = JSON.parse(raw) as Column[];
    if (!Array.isArray(parsed) || parsed.length === 0) return [...DEFAULT_COLUMNS];
    return normalizeColumns(parsed);
  } catch {
    return [...DEFAULT_COLUMNS];
  }
}

// normalizeColumns sanitizes a parsed column array, drops unknown shapes,
// and ensures every built-in is present (hidden if the user removed it).
// Applies persisted width overrides.
export function normalizeColumns(parsed: Column[]): Column[] {
  const out: Column[] = [];
  for (const c of parsed) {
    if (typeof c?.id !== "string") continue;
    if (c.kind !== "builtin" && c.kind !== "field") continue;
    out.push({
      id: c.id,
      label: c.label || (c.kind === "field" ? c.id : c.id),
      kind: c.kind,
      width: typeof c.width === "number" ? c.width : 0,
      visible: c.visible !== false,
    });
  }
  for (const id of Object.keys(BUILTINS)) {
    if (!out.find((c) => c.id === id)) {
      const def = DEFAULT_COLUMNS.find((d) => d.id === id)!;
      out.push({ ...def, visible: false });
    }
  }
  applyWidthOverrides(out);
  return out;
}

export function saveColumns(cols: Column[]) {
  try {
    localStorage.setItem(KEY_COLUMNS, JSON.stringify(cols));
    const widths: Record<string, number> = {};
    for (const c of cols) widths[c.id] = c.width;
    localStorage.setItem(KEY_WIDTHS, JSON.stringify(widths));
  } catch {}
}

// Per-source overrides ---------------------------------------------------

export type ColumnsBySource = Record<string, Column[]>;

export function loadColumnsBySource(): ColumnsBySource {
  try {
    const raw = localStorage.getItem(KEY_BY_SOURCE);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as Record<string, Column[]>;
    const out: ColumnsBySource = {};
    for (const [k, v] of Object.entries(parsed)) {
      if (Array.isArray(v)) out[k] = normalizeColumns(v);
    }
    return out;
  } catch {
    return {};
  }
}

export function saveColumnsBySource(map: ColumnsBySource) {
  try {
    localStorage.setItem(KEY_BY_SOURCE, JSON.stringify(map));
  } catch {}
}

// columnsFromIds materializes a Column[] from a server-issued id list
// (typically `SourceEvent.Columns` after auto-detection). Built-in ids
// reuse the BUILTINS defaults; `@dotted.path` ids and unknown logical
// ids fall through to a generic field column. Built-ins not in the
// recommendation are still appended hidden so the user can re-enable
// them from the column menu.
export function columnsFromIds(ids: string[]): Column[] {
  const out: Column[] = [];
  const seen = new Set<string>();
  // "source" is rendered from source_id, not a JSON field — always include
  // it ahead of caller so the user can tell sources apart at a glance.
  if (!ids.includes("source")) {
    out.push({ id: "source", label: "source", kind: "builtin", width: 100, visible: true });
    seen.add("source");
  }
  for (const id of ids) {
    if (seen.has(id)) continue;
    seen.add(id);
    if (id in BUILTINS) {
      const def = DEFAULT_COLUMNS.find((d) => d.id === id)!;
      out.push({ ...def, visible: true });
    } else if (id.startsWith("@")) {
      out.push({ id, label: id.slice(1), kind: "field", width: 120, visible: true });
    } else {
      // Unknown logical id from a newer server. Fall back to a field
      // column; readFieldPath handles render via aliasMembers.
      out.push({ id, label: id, kind: "field", width: 120, visible: true });
    }
  }
  for (const builtin of DEFAULT_COLUMNS) {
    if (!seen.has(builtin.id)) {
      out.push({ ...builtin, visible: false });
    }
  }
  applyWidthOverrides(out);
  return out;
}

// effectiveColumns picks the column set to render given the set of
// visible source keys, per-source overrides, and the baseline.
//
//   - 0 visible sources    → baseline
//   - 1 visible source     → that source's override, or baseline if none
//   - 2+ visible sources   → union of overrides, with the intersection
//                            (columns present in ALL of them) leading,
//                            then per-source extras. Capped at 9 visible
//                            columns to avoid horizontal scroll. If no
//                            source has an override, baseline.
export function effectiveColumns(
  visibleSourceKeys: string[],
  prefsByKey: ColumnsBySource,
  baseline: Column[],
): Column[] {
  if (visibleSourceKeys.length === 0) return baseline;
  if (visibleSourceKeys.length === 1) {
    return prefsByKey[visibleSourceKeys[0]] ?? baseline;
  }
  const sets = visibleSourceKeys.map((k) => prefsByKey[k]).filter((s): s is Column[] => Array.isArray(s));
  if (sets.length === 0) return baseline;

  // Intersection by id, preserving the order of the first set's visible columns.
  const visibleIds = (cs: Column[]) => new Set(cs.filter((c) => c.visible).map((c) => c.id));
  const allVisible = sets.map(visibleIds);
  const intersection = sets[0]
    .filter((c) => c.visible && allVisible.every((s) => s.has(c.id)))
    .map((c) => c.id);

  // Per-source extras follow the intersection, ordered by source then
  // declaration order within the source.
  const extras: string[] = [];
  const seen = new Set<string>(intersection);
  for (const cs of sets) {
    for (const c of cs) {
      if (!c.visible || seen.has(c.id)) continue;
      seen.add(c.id);
      extras.push(c.id);
    }
  }

  const ids = [...intersection, ...extras].slice(0, 9);
  return columnsFromIds(ids);
}

function applyWidthOverrides(cols: Column[]) {
  try {
    const raw = localStorage.getItem(KEY_WIDTHS);
    if (!raw) return;
    const widths = JSON.parse(raw) as Record<string, number>;
    for (const c of cols) {
      if (typeof widths[c.id] === "number") c.width = widths[c.id]!;
    }
  } catch {}
}

// fromProfileIDs rebuilds a Column[] from a profile's `Columns []string`
// list. Width and visibility come from defaults; custom @field IDs get
// a sensible default width.
export function fromProfileIDs(ids: string[]): Column[] {
  return columnsFromIds(ids);
}

// toProfileIDs returns the visible column IDs in current order — what
// gets persisted to the active profile.
export function toProfileIDs(cols: Column[]): string[] {
  return cols.filter((c) => c.visible).map((c) => c.id);
}

// readFieldPath walks `entry.fields` (and entry top-level slots for the
// built-in aliases) to pull a string value out for a given column id.
//
//   - Built-in slots (ts/msg/level/service) come from entry.* with an
//     alias fallback through entry.fields[<member>] when the slot is
//     empty. This is what makes a Python `timestamp` row render inside
//     the same Time column as a Go `ts` row.
//   - "@dotted.path" ids walk entry.fields directly.
//   - Logical ids (the alias-map keys, e.g. "caller") walk through
//     `aliasMembers` until a non-empty value is found.
export function readFieldPath(fields: unknown, path: string): string {
  if (!fields || typeof fields !== "object") return "";
  let cur: unknown = fields;
  for (const seg of path.split(".")) {
    if (cur == null || typeof cur !== "object") return "";
    cur = (cur as Record<string, unknown>)[seg];
  }
  if (cur == null) return "";
  if (typeof cur === "object") return JSON.stringify(cur);
  return String(cur);
}

// readEntryColumn returns the displayable string for column `id` against
// `entry`. Centralizes the alias chain so renderers don't each implement
// it. Empty string when nothing is set.
export function readEntryColumn(entry: Entry, id: string): string {
  // Built-in top-level slots.
  if (id === "ts") {
    if (entry.ts) return String(entry.ts);
    const fromField = readFieldPath(entry.fields, "timestamp") || readFieldPath(entry.fields, "@timestamp");
    return fromField;
  }
  if (id === "level") return entry.level ?? "";
  if (id === "service") return entry.service ?? "";
  if (id === "msg") {
    if (entry.msg) return entry.msg;
    return readFieldPath(entry.fields, "message");
  }
  // Logical id with an alias chain (e.g. "caller").
  if (isLogicalId(id)) {
    for (const member of aliasMembers(id)) {
      const v = readFieldPath(entry.fields, member);
      if (v) return v;
    }
    return "";
  }
  // "@dotted.path" field column.
  if (id.startsWith("@")) {
    return readFieldPath(entry.fields, id.slice(1));
  }
  // Unknown — try as a raw top-level key.
  return readFieldPath(entry.fields, id);
}

// parseTimestamp turns a string value into a Unix-seconds float, supporting
// ISO 8601 (`2026-05-17T10:15:36.249Z`), numeric strings, and bare numbers.
// Returns NaN if unparseable. Used by the Time column when entry.ts is 0
// but entry.fields.timestamp carries a Python/Node ISO string.
export function parseTimestamp(v: string | number): number {
  if (typeof v === "number") return v;
  if (!v) return NaN;
  const n = Number(v);
  if (Number.isFinite(n)) return n > 1e12 ? n / 1000 : n;
  const ms = Date.parse(v);
  return Number.isFinite(ms) ? ms / 1000 : NaN;
}

// Surfaces of the alias machinery for callers (LogRow, FacetPanel) without
// pulling them into ./aliases directly — keeps the public surface tight.
export { resolveAlias, isLogicalId, aliasMembers, ALIAS_MAP, PRIORITIES } from "./aliases";
