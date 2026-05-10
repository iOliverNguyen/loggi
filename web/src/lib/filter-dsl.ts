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
  // ts and source are always bare (synthetic UI fields), regardless of
  // what the server reports as pre-allocated hot columns.
  BARE_FIELDS.add("ts");
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
      // value is "lo..hi"
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
  const flags = value.slice(end);
  const body = value.slice(1, end - 1);
  if (body === "") return null;
  return { pattern: body.replace(/\\\//g, "/"), flags };
}

function parseTerm(raw: string): Clause | null {
  let negate = false;
  let s = raw;
  if (s.startsWith("-") && s.length > 1) {
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

  // Range: [lo..hi]
  if (value.startsWith("[") && value.endsWith("]")) {
    const inner = value.slice(1, -1);
    const dotdot = inner.indexOf("..");
    if (dotdot < 0) return null;
    if (negate) return null; // not representable as a single negated chip
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
// `lo` and `hi` are both finite, a fresh `ts:[lo..hi]` clause is
// appended. lo/hi are unix seconds.
//
// This lets the timeline brush coexist with a typed filter — the brush
// owns the ts term, everything else is preserved verbatim.
export function withTimeRange(expr: string, lo: number | null, hi: number | null): string {
  const tokens = tokenize(expr.trim());
  const kept: string[] = [];
  for (const t of tokens) {
    let bare = t;
    if (bare.startsWith("-")) bare = bare.slice(1);
    if (bare.startsWith("@")) bare = bare.slice(1);
    if (bare === "ts" || bare.startsWith("ts:")) continue;
    kept.push(t);
  }
  if (lo != null && hi != null && Number.isFinite(lo) && Number.isFinite(hi) && lo < hi) {
    kept.push(`ts:[${Math.floor(lo)}..${Math.ceil(hi)}]`);
  }
  return kept.join(" ");
}
