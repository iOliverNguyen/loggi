// Column configuration for the log list. A `Column` is either a
// built-in (special renderer for ts/level/source/service/msg) or a
// user-added field column that pulls a dotted JSON path out of
// `entry.fields`.
//
// Visibility and order live in `loggi.columns.v1`. Widths live in
// `loggi.columns.widths.v1` keyed by column id. Profile.Columns
// (server-side TOML) round-trips just the ID list — widths are
// per-device by design.

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
  msg: { label: "message", width: 0 },
};

export const DEFAULT_COLUMNS: Column[] = [
  { id: "ts", label: "time", kind: "builtin", width: 96, visible: true },
  { id: "level", label: "level", kind: "builtin", width: 56, visible: true },
  { id: "source", label: "source", kind: "builtin", width: 100, visible: true },
  { id: "service", label: "service", kind: "builtin", width: 128, visible: true },
  { id: "msg", label: "message", kind: "builtin", width: 0, visible: true },
];

const KEY_COLUMNS = "loggi.columns.v1";
const KEY_WIDTHS = "loggi.columns.widths.v1";

export function loadColumns(): Column[] {
  try {
    const raw = localStorage.getItem(KEY_COLUMNS);
    if (!raw) return [...DEFAULT_COLUMNS];
    const parsed = JSON.parse(raw) as Column[];
    if (!Array.isArray(parsed) || parsed.length === 0) return [...DEFAULT_COLUMNS];
    // Drop unknown shapes.
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
    // Make sure every built-in is present (hidden if user removed it).
    for (const id of Object.keys(BUILTINS)) {
      if (!out.find((c) => c.id === id)) {
        const def = DEFAULT_COLUMNS.find((d) => d.id === id)!;
        out.push({ ...def, visible: false });
      }
    }
    applyWidthOverrides(out);
    return out;
  } catch {
    return [...DEFAULT_COLUMNS];
  }
}

export function saveColumns(cols: Column[]) {
  try {
    localStorage.setItem(KEY_COLUMNS, JSON.stringify(cols));
    const widths: Record<string, number> = {};
    for (const c of cols) widths[c.id] = c.width;
    localStorage.setItem(KEY_WIDTHS, JSON.stringify(widths));
  } catch {}
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
  const out: Column[] = [];
  for (const id of ids) {
    if (id in BUILTINS) {
      const def = DEFAULT_COLUMNS.find((d) => d.id === id)!;
      out.push({ ...def, visible: true });
    } else if (id.startsWith("@")) {
      out.push({ id, label: id.slice(1), kind: "field", width: 120, visible: true });
    }
  }
  // Append any built-ins the profile omitted, hidden.
  for (const builtin of DEFAULT_COLUMNS) {
    if (!out.find((c) => c.id === builtin.id)) {
      out.push({ ...builtin, visible: false });
    }
  }
  applyWidthOverrides(out);
  return out;
}

// toProfileIDs returns the visible column IDs in current order — what
// gets persisted to the active profile.
export function toProfileIDs(cols: Column[]): string[] {
  return cols.filter((c) => c.visible).map((c) => c.id);
}

// Read a dotted-path value out of entry.fields. Returns "" for missing.
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
