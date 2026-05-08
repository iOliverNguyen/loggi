<script lang="ts">
  import { tick } from "svelte";
  import Icon from "./Icon.svelte";

  type Tab = "keys" | "syntax" | "examples";
  let { onClose, initialTab } = $props<{ onClose: () => void; initialTab?: Tab }>();
  let dialogEl: HTMLDivElement | null = $state(null);
  // Focus the dialog on mount so keystrokes (←/→ / 1/2/3 / Esc) reach our
  // onkeydown handler instead of being swallowed by the document body.
  $effect(() => {
    if (dialogEl) tick().then(() => dialogEl?.focus());
  });
  const TABS: { id: Tab; label: string }[] = [
    { id: "keys", label: "Shortcuts" },
    { id: "syntax", label: "Filter syntax" },
    { id: "examples", label: "Examples" },
  ];
  let tab = $state<Tab>(initialTab ?? "keys");

  // Modal-level keyboard nav: ←/→ or [ / ] to switch tabs; 1/2/3 jumps
  // directly. Tab is left to the browser for focus traversal between
  // interactive elements within the modal.
  function onKey(e: KeyboardEvent) {
    if (e.key === "Escape") {
      onClose();
      return;
    }
    const idx = TABS.findIndex((t) => t.id === tab);
    if (e.key === "ArrowRight" || e.key === "]") {
      e.preventDefault();
      tab = TABS[(idx + 1) % TABS.length].id;
    } else if (e.key === "ArrowLeft" || e.key === "[") {
      e.preventDefault();
      tab = TABS[(idx - 1 + TABS.length) % TABS.length].id;
    } else if (e.key >= "1" && e.key <= String(TABS.length)) {
      e.preventDefault();
      tab = TABS[parseInt(e.key, 10) - 1].id;
    }
  }

  const SHORTCUTS: { keys: string[]; label: string }[] = [
    { keys: ["/"], label: "focus filter" },
    { keys: ["Tab"], label: "accept filter autocomplete suggestion" },
    { keys: ["⌘F"], label: "highlight substring in messages" },
    { keys: ["Esc"], label: "blur input · close panel · close overlay" },
    { keys: ["j", "k"], label: "row down / up" },
    { keys: ["g", "G"], label: "jump to top / bottom" },
    { keys: ["Enter"], label: "open detail panel" },
    { keys: ["Space"], label: "pause / resume" },
    { keys: ["right-click"], label: "row context menu (↑↓ to navigate, Enter to fire)" },
    { keys: ["⌘L"], label: "copy share URL" },
    { keys: ["⌘C"], label: "copy selected rows as JSONL" },
    { keys: ["p"], label: "pin / unpin row" },
    { keys: ["d"], label: "diff 2 selected rows" },
    { keys: ["⌘click", "⇧click"], label: "multi-select" },
    { keys: ["⌥1", "⌥9"], label: "switch profile by index" },
    { keys: ["⇧1", "⇧9"], label: "apply Nth saved quick filter" },
    { keys: ["←", "→"], label: "switch tabs (in modals / source picker)" },
    { keys: ["?"], label: "this overlay" },
  ];

  const SYNTAX: { title: string; rows: { e: string; d: string }[] }[] = [
    {
      title: "Terms",
      rows: [
        { e: "error", d: "bare word → contains in msg (Datadog-style)" },
        { e: "level:error", d: "exact match on a built-in field" },
        { e: '"connection refused"', d: "quoted string, exact match in msg" },
        { e: "@user.id:42", d: "nested JSON path (use @ for dotted paths)" },
        { e: "[ticket]", d: "bare bracketed text → msg substring" },
        { e: "-->", d: "bare punctuation → msg substring" },
      ],
    },
    {
      title: "Wildcards & negation",
      rows: [
        { e: "msg:*timeout*", d: "substring match on msg" },
        { e: "service:auth*", d: "prefix match" },
        { e: "service:*", d: "field is set (non-empty)" },
        { e: "-level:debug", d: "negation (does not match)" },
        { e: "-*health*", d: "negated substring on msg" },
        { e: 'msg:"*timeout*"', d: "glob inside a quoted string (preserves spaces)" },
        { e: 'msg:"\\*"', d: "literal asterisk via \\*" },
      ],
    },
    {
      title: "Comparisons",
      rows: [
        { e: "level:>=warn", d: "ordinal comparison on level (debug<info<warn<error<fatal)" },
        { e: "@status:>=400", d: "numeric comparison on a JSON field" },
        { e: "ts:[1700000000..1700001000]", d: "range, inclusive" },
      ],
    },
    {
      title: "Combinators",
      rows: [
        { e: "level:error service:auth", d: "implicit AND (whitespace-separated)" },
        { e: "level:warn OR level:error", d: "explicit OR" },
        { e: "(level:warn OR level:error) -service:health", d: "parens + negation" },
      ],
    },
    {
      title: "Built-in fields",
      rows: [
        { e: "level msg ts service env version", d: "" },
        { e: "source caller callerFunc trace_id", d: "" },
      ],
    },
  ];

  const EXAMPLES: { label: string; expr: string }[] = [
    { label: "Errors and worse", expr: "level:>=error" },
    { label: "Auth service warnings or above", expr: "service:auth level:>=warn" },
    { label: "All HTTP 5xx", expr: "@status:>=500" },
    { label: "Slow requests in last hour", expr: "@duration_ms:>=1000" },
    { label: "Trace lookup", expr: "trace_id:abc123" },
    { label: "Anything mentioning timeout", expr: "*timeout*" },
    { label: "Errors but not health checks", expr: "level:>=error -*health*" },
    { label: "Two services together", expr: "service:auth OR service:gateway" },
  ];
</script>

<div
  class="fixed inset-0 bg-black/40 z-40 flex items-center justify-center"
  role="button"
  tabindex="-1"
  onclick={onClose}
  onkeydown={onKey}>
  <div
    bind:this={dialogEl}
    class="bg-white dark:bg-zinc-900 rounded-lg shadow-xl w-[560px] max-h-[80vh] flex flex-col text-sm outline-none"
    role="dialog"
    tabindex="-1"
    onclick={(e) => e.stopPropagation()}
    onkeydown={(e) => { e.stopPropagation(); onKey(e); }}>

    <header class="flex items-center justify-between px-4 py-2.5 border-b border-zinc-200 dark:border-zinc-800">
      <h2 class="font-semibold">Help</h2>
      <button
        class="text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100"
        onclick={onClose}
        aria-label="close">
        <Icon name="x" size={16} />
      </button>
    </header>

    <div class="flex border-b border-zinc-200 dark:border-zinc-800 px-2" role="tablist">
      {#each TABS as t, i}
        <button
          role="tab"
          aria-selected={tab === t.id}
          class="px-3 py-2 -mb-px border-b-2 text-xs transition-colors inline-flex items-center gap-1.5"
          class:border-sky-500={tab === t.id}
          class:text-sky-600={tab === t.id}
          class:dark:text-sky-400={tab === t.id}
          class:border-transparent={tab !== t.id}
          class:text-zinc-500={tab !== t.id}
          onclick={() => (tab = t.id)}>
          {t.label}
          <kbd class="mono text-[9px] px-1 rounded bg-zinc-100 dark:bg-zinc-800 text-zinc-500">{i + 1}</kbd>
        </button>
      {/each}
      <span class="ml-auto self-center text-[10px] text-zinc-400 mono pr-1">← →</span>
    </div>

    <div class="flex-1 overflow-y-auto p-4">
      {#if tab === "keys"}
        <ul class="space-y-1.5 text-xs">
          {#each SHORTCUTS as s}
            <li class="flex justify-between items-center gap-3">
              <span class="text-zinc-700 dark:text-zinc-300">{s.label}</span>
              <span class="flex gap-1">
                {#each s.keys as k}
                  <kbd class="mono px-1.5 py-0.5 rounded bg-zinc-100 dark:bg-zinc-800 border border-zinc-200 dark:border-zinc-700 text-[10px]">{k}</kbd>
                {/each}
              </span>
            </li>
          {/each}
        </ul>
      {:else if tab === "syntax"}
        <div class="space-y-4">
          {#each SYNTAX as group}
            <section>
              <h3 class="text-[10px] uppercase tracking-wider text-zinc-500 font-semibold mb-1.5">{group.title}</h3>
              <ul class="space-y-1 text-xs">
                {#each group.rows as r}
                  <li class="grid grid-cols-[minmax(0,_1fr)_minmax(0,_1.4fr)] gap-3 items-start">
                    <code class="mono text-[11px] px-1.5 py-0.5 rounded bg-zinc-100 dark:bg-zinc-800 break-all">{r.e}</code>
                    {#if r.d}<span class="text-zinc-600 dark:text-zinc-400 leading-snug">{r.d}</span>{:else}<span></span>{/if}
                  </li>
                {/each}
              </ul>
            </section>
          {/each}
        </div>
      {:else if tab === "examples"}
        <ul class="space-y-1.5 text-xs">
          {#each EXAMPLES as e}
            <li class="flex justify-between gap-3 items-start">
              <span class="text-zinc-700 dark:text-zinc-300">{e.label}</span>
              <code class="mono text-[11px] px-1.5 py-0.5 rounded bg-zinc-100 dark:bg-zinc-800 shrink-0 break-all max-w-[60%]">{e.expr}</code>
            </li>
          {/each}
          <li class="text-[11px] text-zinc-500 pt-3 border-t border-zinc-200 dark:border-zinc-800 mt-3">
            Tip: click a value in the detail panel <span class="mono text-emerald-600 dark:text-emerald-400">+</span> button to add it as a clause.
          </li>
        </ul>
      {/if}
    </div>
  </div>
</div>
