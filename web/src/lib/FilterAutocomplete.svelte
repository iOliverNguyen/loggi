<script lang="ts">
  // Lightweight autocomplete that mounts under a filter input. Renders a
  // panel of suggestions filtered by the current word at caret. Tab / Enter
  // accept the highlighted suggestion; ↑/↓ navigate; Esc closes.
  //
  // Suggestion sources, in order:
  //   1. Built-in fields (level, msg, ts, service, etc.) when the current
  //      word looks like a field-prefix (no `:` yet).
  //   2. Common operators / values when the current word starts with a
  //      known field followed by `:` — e.g. `level:` → info|warn|error|…
  //   3. Discovered fields (passed in) — fields the client has seen on
  //      ingested rows, prefixed with `@`.

  import { tick } from "svelte";
  import { BARE_FIELDS as BARE_FIELDS_SET } from "./filter-dsl";

  type Suggestion = { text: string; kind: "field" | "value" | "history" };

  let {
    inputEl,
    value,
    discoveredFields,
    fieldValues,
    recentFilters,
    onChange,
  } = $props<{
    inputEl: HTMLInputElement | null;
    value: string;
    discoveredFields: Set<string>;
    fieldValues?: Map<string, Map<string, number>>;
    recentFilters?: string[];
    onChange: (v: string) => void;
  }>();

  const LEVEL_VALUES = ["debug", "info", "warn", "warning", "error", "fatal"];
  const OPERATORS = [">=", ">", "<=", "<", "*", '"'];

  let open = $state(false);
  let highlight = $state(0);
  let panelEl: HTMLDivElement | null = $state(null);
  let panelPos = $state({ left: 0, top: 0, width: 0 });

  // Compute fixed-positioned coords from the input's bounding rect so the
  // panel escapes the header's stacking context (otherwise the log-list
  // stripes paint over it). Recomputed on open / scroll / resize.
  function place() {
    if (!inputEl) return;
    const r = inputEl.getBoundingClientRect();
    panelPos = { left: r.left, top: r.bottom + 4, width: r.width };
  }

  // Word at caret = the whitespace-bounded chunk under the cursor.
  function wordAt(s: string, caret: number): { word: string; start: number; end: number } {
    let start = caret;
    while (start > 0 && !/\s/.test(s[start - 1] ?? "")) start--;
    let end = caret;
    while (end < s.length && !/\s/.test(s[end] ?? "")) end++;
    return { word: s.slice(start, end), start, end };
  }

  function topValuesForField(field: string, prefix: string, limit: number): string[] {
    const base = field.startsWith("@") ? field.slice(1) : field;
    const m = fieldValues?.get(base);
    if (!m) return [];
    const lower = prefix.toLowerCase();
    const matches = [...m.entries()]
      .filter(([v]) => v.toLowerCase().startsWith(lower))
      .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
      .slice(0, limit)
      .map(([v]) => v);
    return matches;
  }

  function quoteIfNeeded(v: string): string {
    if (/[\s:()[\]"\\]/.test(v)) {
      return `"${v.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
    }
    return v;
  }

  let suggestions = $derived.by((): Suggestion[] => {
    if (!inputEl) return [];
    const caret = inputEl.selectionStart ?? value.length;
    const { word } = wordAt(value, caret);
    const trimmedAll = value.trim();

    // Whole-input is empty + focused → show recent filters as the
    // primary suggestion list.
    if (!trimmedAll && (recentFilters?.length ?? 0) > 0) {
      return recentFilters!.slice(0, 12).map((t: string) => ({ text: t, kind: "history" as const }));
    }

    // While typing, also surface history entries that contain the
    // current input as a substring (case-insensitive). These appear
    // *after* field/value suggestions.
    const historyHits: Suggestion[] = ((recentFilters as string[] | undefined) ?? [])
      .filter((h: string) => h !== trimmedAll && h.toLowerCase().includes(trimmedAll.toLowerCase()))
      .slice(0, 5)
      .map((t: string) => ({ text: t, kind: "history" as const }));

    const bareFields = [...BARE_FIELDS_SET];
    if (!word) {
      // Empty word at caret with non-empty input — show top-level field
      // names plus history hits.
      return [
        ...bareFields.slice(0, 8).map((t) => ({ text: t, kind: "field" as const })),
        ...historyHits,
      ];
    }
    const colon = word.indexOf(":");
    if (colon === -1) {
      // Field prefix — suggest built-ins + discovered fields prefixed with @.
      const stripped = word.startsWith("-") ? word.slice(1) : word;
      const prefix = word.startsWith("-") ? "-" : "";
      const stripped2 = stripped.startsWith("@") ? stripped.slice(1) : stripped;
      const fieldSuggs: Suggestion[] = [
        ...bareFields.filter((f) => f.startsWith(stripped2.toLowerCase())).map((f) => ({ text: prefix + f, kind: "field" as const })),
        ...[...discoveredFields]
          .filter((f) => f.toLowerCase().includes(stripped2.toLowerCase()))
          .slice(0, 10)
          .map((f) => ({ text: prefix + "@" + f, kind: "field" as const })),
      ];
      return [...fieldSuggs.slice(0, 12), ...historyHits];
    }
    // After `field:` — suggest values per field.
    const negPrefix = word.startsWith("-") ? "-" : "";
    const fieldRaw = word.slice(negPrefix.length, colon);
    const valPrefix = word.slice(colon + 1);
    const valueSuggs: Suggestion[] = [];
    if (fieldRaw === "level" || fieldRaw === "-level") {
      // Allow `level:` and `level:>=` style — show operator+value combos.
      if (valPrefix.startsWith(">=") || valPrefix.startsWith("<=") || valPrefix.startsWith(">") || valPrefix.startsWith("<")) {
        const op = valPrefix.match(/^(>=|<=|>|<)/)![0];
        const v = valPrefix.slice(op.length);
        for (const l of LEVEL_VALUES.filter((l) => l.startsWith(v))) {
          valueSuggs.push({ text: `${negPrefix}level:${op}${l}`, kind: "value" });
        }
      } else {
        for (const l of LEVEL_VALUES.filter((l) => l.startsWith(valPrefix))) {
          valueSuggs.push({ text: `${negPrefix}level:${l}`, kind: "value" });
        }
        for (const o of OPERATORS.filter((o) => o.startsWith(valPrefix)).slice(0, 4)) {
          valueSuggs.push({ text: `${negPrefix}level:${o}`, kind: "value" });
        }
      }
    } else {
      // Other fields — pull observed values from fieldValues.
      const top = topValuesForField(fieldRaw, valPrefix, 10);
      for (const v of top) {
        valueSuggs.push({ text: `${negPrefix}${fieldRaw}:${quoteIfNeeded(v)}`, kind: "value" });
      }
      // Always offer the existence predicate.
      if (valPrefix === "" || "*".startsWith(valPrefix)) {
        valueSuggs.push({ text: `${negPrefix}${fieldRaw}:*`, kind: "value" });
      }
    }
    return [...valueSuggs, ...historyHits];
  });

  $effect(() => {
    open = suggestions.length > 0 && document.activeElement === inputEl;
    highlight = 0;
    if (open) place();
  });

  // Keyboard handler attached to the parent input via inputEl.
  function onInputKey(e: KeyboardEvent) {
    if (!open) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      highlight = Math.min(suggestions.length - 1, highlight + 1);
      scrollIntoView();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      highlight = Math.max(0, highlight - 1);
      scrollIntoView();
    } else if (e.key === "Tab" || (e.key === "Enter" && e.shiftKey)) {
      // Tab accepts; Shift+Enter accepts without applying (for chaining).
      // Plain Enter is reserved for the input's own apply handler.
      if (suggestions[highlight]) {
        e.preventDefault();
        accept(suggestions[highlight]);
      }
    } else if (e.key === "Escape") {
      open = false;
    }
  }

  function accept(s: Suggestion) {
    if (!inputEl) return;
    if (s.kind === "history") {
      // Replace the entire input — history entries are full filter
      // expressions, not caret-word completions.
      onChange(s.text);
      tick().then(() => {
        inputEl?.focus();
        const end = s.text.length;
        inputEl?.setSelectionRange(end, end);
      });
      return;
    }
    const caret = inputEl.selectionStart ?? value.length;
    const { start, end } = wordAt(value, caret);
    const next = value.slice(0, start) + s.text + value.slice(end);
    onChange(next);
    tick().then(() => {
      const newCaret = start + s.text.length;
      inputEl?.focus();
      inputEl?.setSelectionRange(newCaret, newCaret);
    });
  }

  function scrollIntoView() {
    requestAnimationFrame(() => {
      const li = panelEl?.querySelector(`[data-idx="${highlight}"]`) as HTMLElement | null;
      li?.scrollIntoView({ block: "nearest" });
    });
  }

  // Close on outside click.
  function onWinClick(e: MouseEvent) {
    if (!open) return;
    const t = e.target as Node | null;
    if (t && (panelEl?.contains(t) || inputEl?.contains(t))) return;
    open = false;
  }

  // Wire the input's keydown via a side-effect; we don't own the input
  // element, just attach a capturing listener.
  let blurTimer: ReturnType<typeof setTimeout> | null = null;
  function onInputFocus() {
    open = suggestions.length > 0;
  }
  function onInputBlur() {
    if (blurTimer !== null) clearTimeout(blurTimer);
    blurTimer = setTimeout(() => {
      open = false;
      blurTimer = null;
    }, 100);
  }
  $effect(() => {
    if (!inputEl) return;
    const el = inputEl;
    el.addEventListener("keydown", onInputKey);
    el.addEventListener("focus", onInputFocus);
    el.addEventListener("blur", onInputBlur);
    return () => {
      el.removeEventListener("keydown", onInputKey);
      el.removeEventListener("focus", onInputFocus);
      el.removeEventListener("blur", onInputBlur);
      if (blurTimer !== null) {
        clearTimeout(blurTimer);
        blurTimer = null;
      }
    };
  });
</script>

<svelte:window onclick={onWinClick} onresize={place} onscroll={place} />

{#if open && suggestions.length > 0}
  <div
    bind:this={panelEl}
    class="fixed z-[60] rounded-md shadow-lg bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 max-h-64 overflow-y-auto text-xs"
    style={`left:${panelPos.left}px;top:${panelPos.top}px;width:${panelPos.width}px`}
    role="listbox">
    <div class="px-2.5 py-1 border-b border-zinc-200 dark:border-zinc-800 text-[10px] text-zinc-500 mono flex items-center justify-between">
      <span>suggestions</span>
      <span><kbd class="px-1 rounded bg-zinc-100 dark:bg-zinc-800">Tab</kbd> accept</span>
    </div>
    {#each suggestions as s, i}
      <button
        type="button"
        data-idx={i}
        class="w-full text-left px-2.5 py-1 mono flex items-center justify-between gap-2"
        class:bg-sky-100={i === highlight}
        class:dark:bg-sky-900={i === highlight}
        onmousedown={(e) => { e.preventDefault(); accept(s); }}
        onmouseenter={() => (highlight = i)}>
        <span class="truncate">{s.text}</span>
        {#if s.kind === "history"}
          <span class="text-[9px] text-zinc-400 shrink-0">recent</span>
        {:else if s.kind === "value"}
          <span class="text-[9px] text-zinc-400 shrink-0">value</span>
        {/if}
      </button>
    {/each}
  </div>
{/if}
