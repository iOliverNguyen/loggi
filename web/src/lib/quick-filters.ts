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

// saveCurrentAsQuick prompts for a label and writes a chip with the
// given expression. Returns true on success.
export function saveCurrentAsQuick(currentFilter: string): boolean {
  const expr = currentFilter.trim();
  const label = window.prompt(`Name this quick filter${expr ? "" : " (saving empty filter)"}:`);
  if (!label) return false;
  const trimmed = label.trim();
  if (!trimmed) return false;
  const chips = loadQuickChips();
  if (chips.some((c) => c.label === trimmed)) {
    if (!window.confirm(`Replace existing "${trimmed}"?`)) return false;
    persistQuickChips(chips.map((c) => (c.label === trimmed ? { label: trimmed, expr } : c)));
  } else {
    persistQuickChips([...chips, { label: trimmed, expr }]);
  }
  return true;
}
