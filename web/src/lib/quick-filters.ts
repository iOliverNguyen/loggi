// Shared helpers for the QuickFilters chip row. Exposed so other parts
// of the UI (e.g. the sidebar's "save" button) can persist a chip
// without going through the QuickFilters component itself.

// QuickChip is a named filter snippet. `pinned` chips AND on top of the
// working filter and have an `enabled` toggle (default true). Plain chips
// are one-click apply-as-working-filter.
export type QuickChip = {
  label: string;
  expr: string;
  pinned?: boolean;
  enabled?: boolean;
};

const KEY = "loggi.quick";

export const DEFAULT_CHIPS: QuickChip[] = [
  { label: "all", expr: "" },
  { label: "info+", expr: "level:>=info" },
  { label: "warn+", expr: "level:>=warn" },
  { label: "error+", expr: "level:>=error" },
];

// Custom event fired after persistQuickChips. Components mirroring
// localStorage state should listen for this on `window`.
export const QUICK_CHANGED = "loggi:quick-changed";

export function loadQuickChips(): QuickChip[] {
  try {
    const raw = localStorage.getItem(KEY);
    if (raw) {
      const parsed = JSON.parse(raw);
      if (Array.isArray(parsed) && parsed.every((x: any) => typeof x?.label === "string" && typeof x?.expr === "string")) {
        return parsed.map((x: any) => ({
          label: x.label,
          expr: x.expr,
          pinned: x.pinned === true,
          enabled: x.enabled !== false,
        }));
      }
    }
  } catch {}
  return DEFAULT_CHIPS.map((c) => ({ ...c, pinned: false, enabled: true }));
}

export function setChipPinned(label: string, pinned: boolean): void {
  persistQuickChips(loadQuickChips().map((c) => (c.label === label ? { ...c, pinned } : c)));
}

export function setChipEnabled(label: string, enabled: boolean): void {
  persistQuickChips(loadQuickChips().map((c) => (c.label === label ? { ...c, enabled } : c)));
}

// computeEffectiveFilter ANDs all enabled pinned chips on top of the
// working filter. Whitespace is the implicit AND in the DSL.
export function computeEffectiveFilter(working: string, chips: QuickChip[]): string {
  const parts: string[] = [];
  for (const c of chips) {
    if (!c.pinned || c.enabled === false) continue;
    const e = c.expr.trim();
    if (e) parts.push(`(${e})`);
  }
  const w = working.trim();
  if (w) parts.push(`(${w})`);
  return parts.join(" ");
}

export function persistQuickChips(chips: QuickChip[]): void {
  try {
    localStorage.setItem(KEY, JSON.stringify(chips));
  } catch {}
  window.dispatchEvent(new CustomEvent(QUICK_CHANGED));
}

// QUICK_PROMPT is dispatched on `window` to open the save-as-quick
// dialog. The detail carries the expression to be saved.
export const QUICK_PROMPT = "loggi:save-quick-prompt";

// requestSaveQuick asks the host page to open a save dialog. The
// actual persistence happens through commitQuickChip once the user
// confirms.
export function requestSaveQuick(expr: string): void {
  window.dispatchEvent(new CustomEvent(QUICK_PROMPT, { detail: { expr } }));
}

// commitQuickChip writes a chip; called by the save dialog after the
// user picks a label. Returns whether an existing chip was replaced.
export function commitQuickChip(label: string, expr: string, pinned = false): { ok: boolean; replaced: boolean } {
  const trimmed = label.trim();
  if (!trimmed) return { ok: false, replaced: false };
  const chips = loadQuickChips();
  if (chips.some((c) => c.label === trimmed)) {
    persistQuickChips(chips.map((c) => (c.label === trimmed ? { label: trimmed, expr, pinned, enabled: true } : c)));
    return { ok: true, replaced: true };
  }
  persistQuickChips([...chips, { label: trimmed, expr, pinned, enabled: true }]);
  return { ok: true, replaced: false };
}
