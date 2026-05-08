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

  let {
    inputEl,
    value,
    discoveredFields,
    onChange,
  } = $props<{
    inputEl: HTMLInputElement | null;
    value: string;
    discoveredFields: Set<string>;
    onChange: (v: string) => void;
  }>();

  const BUILTINS = ["level", "msg", "ts", "service", "env", "version", "source", "caller", "callerFunc", "trace_id"];
  const LEVEL_VALUES = ["debug", "info", "warn", "warning", "error", "fatal"];
  const OPERATORS = [">=", ">", "<=", "<", "*", '"'];

  let open = $state(false);
  let highlight = $state(0);
  let panelEl: HTMLDivElement | null = $state(null);

  // Word at caret = the whitespace-bounded chunk under the cursor.
  function wordAt(s: string, caret: number): { word: string; start: number; end: number } {
    let start = caret;
    while (start > 0 && !/\s/.test(s[start - 1] ?? "")) start--;
    let end = caret;
    while (end < s.length && !/\s/.test(s[end] ?? "")) end++;
    return { word: s.slice(start, end), start, end };
  }

  let suggestions = $derived.by((): string[] => {
    if (!inputEl) return [];
    const caret = inputEl.selectionStart ?? value.length;
    const { word } = wordAt(value, caret);
    if (!word) {
      // Empty word at caret — show top-level field names.
      return BUILTINS.slice(0, 8);
    }
    const lower = word.toLowerCase();
    const colon = word.indexOf(":");
    if (colon === -1) {
      // Field prefix — suggest built-ins + discovered fields prefixed with @.
      const stripped = word.startsWith("-") ? word.slice(1) : word;
      const prefix = word.startsWith("-") ? "-" : "";
      const stripped2 = stripped.startsWith("@") ? stripped.slice(1) : stripped;
      const out = [
        ...BUILTINS.filter((f) => f.startsWith(stripped2.toLowerCase())).map((f) => prefix + f),
        ...[...discoveredFields]
          .filter((f) => f.toLowerCase().includes(stripped2.toLowerCase()))
          .slice(0, 10)
          .map((f) => prefix + "@" + f),
      ];
      return out.slice(0, 12);
    }
    // After `field:` — suggest values per field.
    const field = word.slice(0, colon);
    const valPrefix = word.slice(colon + 1);
    if (field === "level") {
      // Allow `level:` and `level:>=` style — show operator+value combos.
      if (valPrefix.startsWith(">=") || valPrefix.startsWith("<=") || valPrefix.startsWith(">") || valPrefix.startsWith("<")) {
        const op = valPrefix.match(/^(>=|<=|>|<)/)![0];
        const v = valPrefix.slice(op.length);
        return LEVEL_VALUES.filter((l) => l.startsWith(v)).map((l) => `level:${op}${l}`);
      }
      return [
        ...LEVEL_VALUES.filter((l) => l.startsWith(valPrefix)).map((l) => `level:${l}`),
        ...OPERATORS.filter((o) => o.startsWith(valPrefix)).slice(0, 4).map((o) => `level:${o}`),
      ];
    }
    return [];
  });

  $effect(() => {
    open = suggestions.length > 0 && document.activeElement === inputEl;
    highlight = 0;
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

  function accept(suggestion: string) {
    if (!inputEl) return;
    const caret = inputEl.selectionStart ?? value.length;
    const { start, end } = wordAt(value, caret);
    const next = value.slice(0, start) + suggestion + value.slice(end);
    onChange(next);
    tick().then(() => {
      const newCaret = start + suggestion.length;
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
  $effect(() => {
    if (!inputEl) return;
    inputEl.addEventListener("keydown", onInputKey);
    inputEl.addEventListener("focus", () => (open = suggestions.length > 0));
    inputEl.addEventListener("blur", () => setTimeout(() => (open = false), 100));
    return () => {
      inputEl?.removeEventListener("keydown", onInputKey);
    };
  });
</script>

<svelte:window onclick={onWinClick} />

{#if open && suggestions.length > 0}
  <div
    bind:this={panelEl}
    class="absolute z-30 left-0 right-0 mt-1 rounded-md shadow-lg bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 max-h-64 overflow-y-auto text-xs"
    role="listbox">
    <div class="px-2.5 py-1 border-b border-zinc-200 dark:border-zinc-800 text-[10px] text-zinc-500 mono flex items-center justify-between">
      <span>suggestions</span>
      <span><kbd class="px-1 rounded bg-zinc-100 dark:bg-zinc-800">Tab</kbd> accept</span>
    </div>
    {#each suggestions as s, i}
      <button
        type="button"
        data-idx={i}
        class="w-full text-left px-2.5 py-1 mono"
        class:bg-sky-100={i === highlight}
        class:dark:bg-sky-900={i === highlight}
        onmousedown={(e) => { e.preventDefault(); accept(s); }}
        onmouseenter={() => (highlight = i)}>{s}</button>
    {/each}
  </div>
{/if}
