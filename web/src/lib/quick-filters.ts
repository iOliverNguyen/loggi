// Shared helpers for the QuickFilters chip row. Exposed so other parts
// of the UI (e.g. the sidebar's "save" button) can persist a chip
// without going through the QuickFilters component itself.

export type QuickChip = { label: string; expr: string };

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
        return parsed;
      }
    }
  } catch {}
  return DEFAULT_CHIPS;
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
export function commitQuickChip(label: string, expr: string): { ok: boolean; replaced: boolean } {
  const trimmed = label.trim();
  if (!trimmed) return { ok: false, replaced: false };
  const chips = loadQuickChips();
  if (chips.some((c) => c.label === trimmed)) {
    persistQuickChips(chips.map((c) => (c.label === trimmed ? { label: trimmed, expr } : c)));
    return { ok: true, replaced: true };
  }
  persistQuickChips([...chips, { label: trimmed, expr }]);
  return { ok: true, replaced: false };
}
