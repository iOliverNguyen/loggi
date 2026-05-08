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
  | "range";

export interface Clause {
  field: string; // dotted path: "level", "service", "user.id"
  op: ClauseOp;
  value: string; // for "range": "lo..hi"
}

// Built-in top-level fields the parser/compiler treats as bare identifiers
// (no @ prefix). Anything else with a dot is rendered as @a.b.c.
const BUILTIN_FIELDS = new Set([
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
  }
}

export function clauseToTerm(c: Clause): string {
  if (c.field === "msg" && (c.op === "contains" || c.op === "ncontains")) {
    // Bare msg substring — Datadog-style, shorter expression.
    const inner = `*${c.value.replace(/\s/g, "")}*`;
    return c.op === "ncontains" ? `-${inner}` : inner;
  }
  const term = `${fieldRef(c.field)}:${renderValue(c.op, c.value)}`;
  if (c.op === "neq" || c.op === "ncontains") return `-${term}`;
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
};

export function defaultOpsForField(field: string): ClauseOp[] {
  if (field === "level") {
    // level is ordinal-comparable in the server.
    return ["eq", "neq", "gte", "gt", "lte", "lt"];
  }
  if (field === "ts") {
    return ["range", "gte", "gt", "lte", "lt"];
  }
  return ["eq", "neq", "contains", "ncontains"];
}

export { BUILTIN_FIELDS };
