// Filter DSL helpers for the chip-based filter builder.
//
// The server-side DSL (see internal/filter/parser.go) supports a richer
// grammar than this helper exposes — OR, parens, NOT, etc. We only round-trip
// a subset: ANDed clauses, optional negation, and a fixed set of operators.
// Anything outside that subset is reported as `advanced: true` and the UI
// shows the raw expression instead of chips.

export type ClauseOp =
  | "eq"
  | "neq"
  | "contains"
  | "ncontains"
  | "gt"
  | "gte"
  | "lt"
  | "lte"
  | "range"
  | "exists"
  | "nexists"
  | "regex"
  | "nregex";

export interface Clause {
  field: string; // dotted path: "level", "service", "user.id"
  op: ClauseOp;
  value: string; // for "range": "lo..hi"
  flags?: string; // for "regex"/"nregex": optional /pat/flags
}

// Well-known top-level fields the parser/compiler treats as bare
// identifiers (no @ prefix). Anything else with a dot is rendered as
// @a.b.c. Distinct from the 5-item UI builtins in `columns.ts`.
//
// The list is mutable so the boot path can replace it with the
// authoritative `well_known` array from `/api/columns`. The hardcoded
// values are a sensible pre-fetch default.
export const BARE_FIELDS = new Set<string>([
  "level",
  "msg",
  "ts",
  "service",
  "env",
  "version",
  "source",
  "caller",
  "callerFunc",
  "trace_id",
]);

export function setBareFields(fields: string[]): void {
  BARE_FIELDS.clear();
  // `source` is the only true synthetic field — the server resolves it
  // from SourceID at materialize time, so it never appears in the
  // well-known list but should still render bare in chip terms.
  BARE_FIELDS.add("source");
  for (const f of fields) BARE_FIELDS.add(f);
}

function fieldRef(field: string): string {
  if (field.includes(".")) return "@" + field;
  return field;
}

function quoteIfNeeded(v: string): string {
  if (v === "") return '""';
  if (/[\s:()[\]"\\*]/.test(v)) {
    return `"${v.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
  }
  return v;
}

// Render the value side of a fieldTerm. For "contains" we wrap with `*`,
// which the (extended) server parser treats as a SubstrNode on the field.
function renderValue(op: ClauseOp, v: string): string {
  switch (op) {
    case "eq":
    case "neq":
      return quoteIfNeeded(v);
    case "contains":
    case "ncontains":
      // Substring: server tokenizer needs to see at least one '*' inside
      // the value token. Avoid quoting — quoted strings parse as exact.
      return `*${v.replace(/\s/g, "")}*`;
    case "gt":
      return `>${v}`;
    case "gte":
      return `>=${v}`;
    case "lt":
      return `<${v}`;
    case "lte":
      return `<=${v}`;
    case "range": {
      // Two value shapes: numeric "lo..hi" (server-bound) and the
      // human-readable ts form "HH:mm:ss.SSS – HH:mm:ss.SSS" (with an
      // optional "Mmm/DD, " prefix on each side) — the latter is preserved
      // verbatim so the chip view round-trips byte-for-byte.
      if (v.includes(" – ")) return `[${v}]`;
      const [lo, hi] = v.split("..");
      return `[${lo}..${hi}]`;
    }
    case "exists":
    case "nexists":
      return "*";
    case "regex":
    case "nregex":
      // value is the bare pattern (no slashes); flags are the trailing chars.
      return `/${v.replace(/\//g, "\\/")}/`;
  }
}

export function clauseToTerm(c: Clause): string {
  if (c.field === "msg" && (c.op === "contains" || c.op === "ncontains")) {
    // Bare msg substring — Datadog-style, shorter expression.
    const inner = `*${c.value.replace(/\s/g, "")}*`;
    return c.op === "ncontains" ? `-${inner}` : inner;
  }
  let rendered = renderValue(c.op, c.value);
  if ((c.op === "regex" || c.op === "nregex") && c.flags) rendered += c.flags;
  const term = `${fieldRef(c.field)}:${rendered}`;
  if (c.op === "neq" || c.op === "ncontains" || c.op === "nexists" || c.op === "nregex") return `-${term}`;
  return term;
}

export function clausesToExpr(clauses: Clause[]): string {
  return clauses.map(clauseToTerm).join(" ");
}

export interface ParseResult {
  clauses: Clause[];
  advanced: boolean; // expression contains constructs we can't round-trip as chips
}

// parseClauses is a best-effort decomposition of a filter expression into
// chip-friendly clauses. It only succeeds on whitespace-separated terms
// without OR / parens / nested negation — the subset the builder produces.
// If anything else is detected, `advanced: true` is returned and the chips
// view should fall back to the raw input.
export function parseClauses(expr: string): ParseResult {
  const trimmed = expr.trim();
  if (trimmed === "") return { clauses: [], advanced: false };
  if (/\(|\)|\bOR\b|\bor\b|\bAND\b|\band\b/.test(trimmed)) {
    return { clauses: [], advanced: true };
  }

  const tokens = tokenize(trimmed);
  const clauses: Clause[] = [];
  for (const t of tokens) {
    const c = parseTerm(t);
    if (!c) return { clauses: [], advanced: true };
    clauses.push(c);
  }
  return { clauses, advanced: false };
}

// Split on whitespace except inside quoted strings or brackets.
function tokenize(s: string): string[] {
  const out: string[] = [];
  let i = 0;
  while (i < s.length) {
    while (i < s.length && /\s/.test(s[i]!)) i++;
    if (i >= s.length) break;
    const start = i;
    let inQuote = false;
    let bracket = 0;
    while (i < s.length) {
      const c = s[i]!;
      if (inQuote) {
        if (c === "\\" && i + 1 < s.length) {
          i += 2;
          continue;
        }
        if (c === '"') inQuote = false;
        i++;
        continue;
      }
      if (c === '"') {
        inQuote = true;
        i++;
        continue;
      }
      if (c === "[") bracket++;
      if (c === "]") bracket = Math.max(0, bracket - 1);
      if (bracket === 0 && /\s/.test(c)) break;
      i++;
    }
    out.push(s.slice(start, i));
  }
  return out;
}

function unquote(v: string): string {
  if (v.length >= 2 && v[0] === '"' && v[v.length - 1] === '"') {
    return v.slice(1, -1).replace(/\\(.)/g, "$1");
  }
  return v;
}

function parseRegexLiteral(value: string): { pattern: string; flags: string } | null {
  if (value.length < 2 || value[0] !== "/") return null;
  let end = value.length;
  while (end > 1 && (value[end - 1] === "i" || value[end - 1] === "g")) end--;
  if (end <= 1 || value[end - 1] !== "/") return null;
  // Drop `g`: the Go server's regex parser rejects it. Without this strip,
  // a `/foo/g` typed in the UI round-trips into a saved chip and explodes
  // on re-apply.
  const flags = value.slice(end).replace(/g/g, "");
  const body = value.slice(1, end - 1);
  if (body === "") return null;
  return { pattern: body.replace(/\\\//g, "/"), flags };
}

function parseTerm(raw: string): Clause | null {
  let negate = false;
  let s = raw;
  if ((s.startsWith("-") || s.startsWith("!")) && s.length > 1) {
    negate = true;
    s = s.slice(1);
  }

  // Bare msg substring: *needle*
  if (s.startsWith("*") && s.endsWith("*") && !s.includes(":")) {
    return {
      field: "msg",
      op: negate ? "ncontains" : "contains",
      value: s.slice(1, -1),
    };
  }

  const colon = s.indexOf(":");
  if (colon < 0) {
    // bare ident → msg substring (Datadog convention)
    return {
      field: "msg",
      op: negate ? "ncontains" : "contains",
      value: unquote(s),
    };
  }

  let field = s.slice(0, colon);
  const value = s.slice(colon + 1);
  if (field.startsWith("@")) field = field.slice(1);
  if (!field) return null;

  // Regex literal: /pattern/flags. Pattern body may contain `\/` to escape
  // a literal slash; we don't decode that here (round-trips back via the
  // server-side parser).
  {
    const rx = parseRegexLiteral(value);
    if (rx) {
      return {
        field,
        op: negate ? "nregex" : "regex",
        value: rx.pattern,
        flags: rx.flags,
      };
    }
  }

  // Range: [lo..hi] or human-form ts [HH:mm:ss.SSS – HH:mm:ss.SSS]
  // (optionally prefixed with "Mmm/DD, " when the endpoints fall on
  // different local dates).
  if (value.startsWith("[") && value.endsWith("]")) {
    const inner = value.slice(1, -1);
    if (negate) return null; // not representable as a single negated chip
    if (inner.includes(" – ")) {
      return { field, op: "range", value: inner };
    }
    const dotdot = inner.indexOf("..");
    if (dotdot < 0) return null;
    return {
      field,
      op: "range",
      value: `${inner.slice(0, dotdot).trim()}..${inner.slice(dotdot + 2).trim()}`,
    };
  }

  // Comparisons
  const cmp = value.match(/^(>=|<=|>|<)(.*)$/);
  if (cmp) {
    if (negate) return null;
    const opMap: Record<string, ClauseOp> = { ">=": "gte", "<=": "lte", ">": "gt", "<": "lt" };
    return { field, op: opMap[cmp[1]!]!, value: cmp[2]!.trim() };
  }

  // Existence predicate: `field:*` (or any all-stars value).
  if (/^\*+$/.test(value)) {
    return { field, op: negate ? "nexists" : "exists", value: "" };
  }

  // Quoted-string globs (e.g. `field:"*foo*"`) round-trip would lose the
  // `\*` escape distinction — punt to advanced.
  if (value.startsWith('"') && value.includes("*")) {
    return null;
  }

  // Substring: *x* or *x or x*
  if (value.includes("*")) {
    return {
      field,
      op: negate ? "ncontains" : "contains",
      value: value.replace(/\*/g, ""),
    };
  }

  return { field, op: negate ? "neq" : "eq", value: unquote(value) };
}

// Available operators for a given field type. Used by the UI to populate
// the operator dropdown.
export const OP_LABELS: Record<ClauseOp, string> = {
  eq: "=",
  neq: "≠",
  contains: "contains",
  ncontains: "!contains",
  gt: ">",
  gte: "≥",
  lt: "<",
  lte: "≤",
  range: "in [a..b]",
  exists: "is set",
  nexists: "not set",
  regex: "matches /…/",
  nregex: "!matches /…/",
};

export function defaultOpsForField(field: string): ClauseOp[] {
  if (field === "level") {
    // level is ordinal-comparable in the server.
    return ["eq", "neq", "gte", "gt", "lte", "lt", "exists", "nexists"];
  }
  if (field === "ts") {
    return ["range", "gte", "gt", "lte", "lt", "exists", "nexists"];
  }
  return ["eq", "neq", "contains", "ncontains", "regex", "nregex", "exists", "nexists"];
}

// Source mute / solo helpers — used by the sidebar M/S toggles.
//
// Mute appends a `-source:NAME` clause. Multiple sources can be muted
// independently. Solo replaces any existing `source:X` (eq) clause with
// `source:NAME` — solo is exclusive because the chip-friendly DSL only
// supports implicit AND, so two solos would produce an empty result.

export function isSourceMuted(expr: string, src: string): boolean {
  const r = parseClauses(expr);
  if (r.advanced) return false;
  return r.clauses.some((c) => c.field === "source" && c.op === "neq" && c.value === src);
}

export function isSourceSoloed(expr: string, src: string): boolean {
  const r = parseClauses(expr);
  if (r.advanced) return false;
  return r.clauses.some((c) => c.field === "source" && c.op === "eq" && c.value === src);
}

export function setSourceMuted(expr: string, src: string, muted: boolean): string {
  const r = parseClauses(expr);
  if (r.advanced) {
    if (muted && !isSourceMuted(expr, src)) {
      const term = clauseToTerm({ field: "source", op: "neq", value: src });
      return expr.trim() ? `${expr.trim()} ${term}` : term;
    }
    return expr; // can't safely remove from an advanced expr
  }
  const next = r.clauses.filter((c) => !(c.field === "source" && c.op === "neq" && c.value === src));
  if (muted) next.push({ field: "source", op: "neq", value: src });
  return clausesToExpr(next);
}

export function setSourceSoloed(expr: string, src: string, soloed: boolean): string {
  const r = parseClauses(expr);
  if (r.advanced) {
    if (soloed && !isSourceSoloed(expr, src)) {
      const term = clauseToTerm({ field: "source", op: "eq", value: src });
      return expr.trim() ? `${expr.trim()} ${term}` : term;
    }
    return expr;
  }
  // Drop any existing source-eq clauses first — solo is exclusive.
  const next = r.clauses.filter((c) => !(c.field === "source" && c.op === "eq"));
  if (soloed) next.push({ field: "source", op: "eq", value: src });
  return clausesToExpr(next);
}

// withTimeRange returns `expr` with any existing `ts:[..]` / `ts:>=` /
// `ts:>` / `ts:<=` / `ts:<` / `ts:N..M` / `-ts:...` clauses removed. If
// `lo` and `hi` are both finite, a fresh `ts:[…]` clause is appended.
// lo/hi are unix seconds.
//
// The emitted clause uses the **human-readable** form so the filter input
// stays scannable. Same local date: `ts:[HH:mm:ss.SSS – HH:mm:ss.SSS]`.
// Crosses local midnight: `ts:[Mmm/DD, HH:mm:ss.SSS – Mmm/DD, HH:mm:ss.SSS]`.
// `compileTsForWire` translates back to `ts:[unix..unix]` just before
// the expression is sent over the wire.
//
// This lets the timeline brush coexist with a typed filter — the brush
// owns the ts term, everything else is preserved verbatim.
export function withTimeRange(expr: string, lo: number | null, hi: number | null): string {
  const tokens = tokenize(expr.trim());
  const kept: string[] = [];
  for (const t of tokens) {
    let bare = t;
    if (bare.startsWith("-") || bare.startsWith("!")) bare = bare.slice(1);
    if (bare.startsWith("@")) bare = bare.slice(1);
    if (bare === "ts" || bare.startsWith("ts:")) continue;
    kept.push(t);
  }
  if (lo != null && hi != null && Number.isFinite(lo) && Number.isFinite(hi) && lo < hi) {
    kept.push(`ts:[${formatTsRange(Math.floor(lo), Math.ceil(hi))}]`);
  }
  return kept.join(" ");
}

const MONTH_NAMES = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

function pad2(n: number): string {
  return n < 10 ? `0${n}` : String(n);
}

function pad3(n: number): string {
  return n < 10 ? `00${n}` : n < 100 ? `0${n}` : String(n);
}

function fmtHMS(d: Date): string {
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}.${pad3(d.getMilliseconds())}`;
}

function fmtDatePrefix(d: Date): string {
  return `${MONTH_NAMES[d.getMonth()]}/${pad2(d.getDate())}, `;
}

// formatTsRange renders the human-readable inner of a `ts:[…]` clause.
// Inputs are unix seconds. The short (no-date-prefix) form is only safe
// when *both* endpoints fall on today's local date — `parseTsEndpoint`'s
// no-prefix path resolves against today, so a yesterday-only brush
// emitted without a prefix would round-trip 24 h ahead and silently
// match nothing on the server.
function formatTsRange(loSec: number, hiSec: number): string {
  const dLo = new Date(loSec * 1000);
  const dHi = new Date(hiSec * 1000);
  const today = new Date();
  const isToday = (d: Date) =>
    d.getFullYear() === today.getFullYear() &&
    d.getMonth() === today.getMonth() &&
    d.getDate() === today.getDate();
  if (isToday(dLo) && isToday(dHi)) {
    return `${fmtHMS(dLo)} – ${fmtHMS(dHi)}`;
  }
  return `${fmtDatePrefix(dLo)}${fmtHMS(dLo)} – ${fmtDatePrefix(dHi)}${fmtHMS(dHi)}`;
}

// Parse one endpoint of a human ts range. Returns unix seconds, or null
// if `s` doesn't match either shape. Same-date endpoints (no Mmm/DD
// prefix) resolve against `defaultY` / `defaultM` / `defaultD` (local
// date). Cross-date endpoints with an explicit Mmm/DD prefix pick the
// year closest to `now` so year-end wraps round-trip without a stored
// year in the visible string.
const ENDPOINT_RE = /^(?:([A-Za-z]{3})\/(\d{2}),\s*)?(\d{2}):(\d{2}):(\d{2})\.(\d{3})$/;

function parseTsEndpoint(
  s: string,
  defaults: { y: number; m: number; d: number },
  nowMs: number,
): number | null {
  const m = ENDPOINT_RE.exec(s.trim());
  if (!m) return null;
  const [, monStr, dayStr, hh, mm, ss, ms] = m;
  const H = Number(hh), M = Number(mm), S = Number(ss), MS = Number(ms);
  if (!monStr) {
    // No date prefix: anchor against today, but if today's HH:MM:SS lands
    // far in the future (> 1 min ahead of now) the filter was almost
    // certainly written before midnight and reloaded the next day. Fall
    // back to yesterday so the round-trip stays correct across the wrap.
    const FUTURE_SLACK_MS = 60_000;
    const t = new Date(defaults.y, defaults.m, defaults.d, H, M, S, MS).getTime();
    if (t > nowMs + FUTURE_SLACK_MS) {
      const tPrev = new Date(defaults.y, defaults.m, defaults.d - 1, H, M, S, MS).getTime();
      return Math.floor(tPrev / 1000);
    }
    return Math.floor(t / 1000);
  }
  const mi = MONTH_NAMES.indexOf(monStr);
  if (mi < 0) return null;
  const day = Number(dayStr);
  // Pick the year (current or current ± 1) whose resulting timestamp
  // is closest to `nowMs` — handles dec/jan wrap symmetrically.
  const curY = new Date(nowMs).getFullYear();
  let best = NaN;
  let bestDelta = Infinity;
  for (const y of [curY - 1, curY, curY + 1]) {
    const t = new Date(y, mi, day, H, M, S, MS).getTime();
    const delta = Math.abs(t - nowMs);
    if (delta < bestDelta) {
      bestDelta = delta;
      best = t;
    }
  }
  return Number.isFinite(best) ? Math.floor(best / 1000) : null;
}

// compileTsForWire rewrites any `ts:[<human>]` clauses in `expr` to
// `ts:[<unix>..<unix>]` so the server's numeric range parser accepts
// them. Numeric `ts:[lo..hi]` clauses pass through unchanged.
//
// Uses a regex (not a tokenizer) so wrappers like `(…)` from
// `computeEffectiveFilter`'s pinned-chip ANDing don't fuse the ts term
// into a bigger token that no longer starts with `ts:[`.
const TS_HUMAN_RE = /(-|!)?ts:\[((?:[A-Za-z]{3}\/\d{2},\s*)?\d{2}:\d{2}:\d{2}\.\d{3}\s+–\s+(?:[A-Za-z]{3}\/\d{2},\s*)?\d{2}:\d{2}:\d{2}\.\d{3})\]/g;

export function compileTsForWire(expr: string): string {
  // Fast path: the human form always contains the en-dash separator.
  if (!expr.includes("–")) return expr;
  const now = Date.now();
  const today = new Date(now);
  const defaults = { y: today.getFullYear(), m: today.getMonth(), d: today.getDate() };
  return expr.replace(TS_HUMAN_RE, (whole, neg: string | undefined, inner: string) => {
    const sep = inner.indexOf("–");
    const loStr = inner.slice(0, sep).trim();
    const hiStr = inner.slice(sep + 1).trim();
    const lo = parseTsEndpoint(loStr, defaults, now);
    const hi = parseTsEndpoint(hiStr, defaults, now);
    if (lo == null || hi == null) return whole; // malformed — let server surface the error
    return `${neg ?? ""}ts:[${lo}..${hi}]`;
  });
}
