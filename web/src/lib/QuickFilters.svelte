<script lang="ts">
  import Icon from "./Icon.svelte";
  import {
    type QuickChip,
    QUICK_CHANGED,
    loadQuickChips,
    persistQuickChips,
    saveCurrentAsQuick,
  } from "./quick-filters";

  let { activeFilter, currentFilter, onApply } = $props<{
    activeFilter: string;
    currentFilter: string;
    onApply: (expr: string) => void;
  }>();

  let chips = $state<QuickChip[]>(loadQuickChips());
  let menuOpen = $state(false);
  let containerEl: HTMLDivElement | null = $state(null);
  let listEl: HTMLDivElement | null = $state(null);
  // Start with a generous default; measure() narrows it once we have real
  // layout. Reading chips.length here captures only the initial size, which
  // is what we want.
  let visibleCount = $state(99);

  // Sync with external mutations (e.g. the sidebar's quick-save button).
  $effect(() => {
    const onChanged = () => (chips = loadQuickChips());
    window.addEventListener(QUICK_CHANGED, onChanged);
    return () => window.removeEventListener(QUICK_CHANGED, onChanged);
  });

  function isActive(expr: string): boolean {
    return expr === activeFilter;
  }

  function saveCurrent() {
    if (saveCurrentAsQuick(currentFilter)) chips = loadQuickChips();
  }

  function remove(label: string) {
    persistQuickChips(chips.filter((c) => c.label !== label));
    chips = loadQuickChips();
  }

  // Measure visible chips: hide chips that overflow the container width and
  // expose the rest under a dropdown. We measure on mount and on resize.
  function measure() {
    if (!listEl || !containerEl) return;
    const containerW = containerEl.clientWidth;
    // Reserve space for the leading label and trailing buttons (~150px).
    const budget = Math.max(0, containerW - 220);
    let used = 0;
    let count = 0;
    const items = Array.from(listEl.children) as HTMLElement[];
    for (const el of items) {
      // Temporarily ensure visibility for measurement.
      const w = el.scrollWidth + 6; // gap
      if (used + w > budget && count > 0) break;
      used += w;
      count++;
    }
    visibleCount = Math.max(1, count);
  }

  $effect(() => {
    if (!containerEl) return;
    const ro = new ResizeObserver(() => measure());
    ro.observe(containerEl);
    measure();
    return () => ro.disconnect();
  });

  // Re-measure when chips list changes.
  $effect(() => {
    void chips.length;
    queueMicrotask(measure);
  });

  let visible = $derived(chips.slice(0, visibleCount));
  let overflow = $derived(chips.slice(visibleCount));
</script>

<div
  bind:this={containerEl}
  class="px-4 py-1.5 border-b border-zinc-200 dark:border-zinc-800 flex items-center gap-2 text-xs"
  onkeydown={(e) => {
    // Arrow keys navigate the chip row when focus is on a chip.
    const t = e.target as HTMLElement | null;
    if (!t || t.tagName !== "BUTTON") return;
    if (e.key === "ArrowRight" || e.key === "ArrowLeft") {
      const buttons = Array.from(listEl?.querySelectorAll<HTMLButtonElement>("button[data-chip]") ?? []);
      const idx = buttons.indexOf(t as HTMLButtonElement);
      if (idx === -1) return;
      e.preventDefault();
      const next = e.key === "ArrowRight" ? (idx + 1) % buttons.length : (idx - 1 + buttons.length) % buttons.length;
      buttons[next]?.focus();
    }
  }}>
  <span class="text-zinc-500 shrink-0">Quick:</span>
  <div bind:this={listEl} class="flex items-center gap-1.5 min-w-0 flex-1 overflow-hidden">
    {#each visible as c, i (c.label)}
      <span class="group relative inline-flex items-center">
        <button
          data-chip
          class="px-2 py-0.5 rounded text-[11px] mono whitespace-nowrap transition-colors"
          class:bg-sky-600={isActive(c.expr)}
          class:text-white={isActive(c.expr)}
          class:bg-zinc-100={!isActive(c.expr)}
          class:dark:bg-zinc-800={!isActive(c.expr)}
          class:hover:bg-zinc-200={!isActive(c.expr)}
          class:dark:hover:bg-zinc-700={!isActive(c.expr)}
          title={`${c.expr || "no filter"}${i < 9 ? ` (Shift+${i + 1})` : ""}`}
          onclick={() => onApply(c.expr)}>{c.label}</button>
        <button
          class="opacity-0 group-hover:opacity-100 absolute -top-1 -right-1 w-3.5 h-3.5 rounded-full bg-zinc-700 dark:bg-zinc-200 text-white dark:text-zinc-900 text-[9px] flex items-center justify-center"
          title="remove"
          onclick={() => remove(c.label)}>×</button>
      </span>
    {/each}
  </div>
  {#if overflow.length > 0}
    <div class="relative shrink-0">
      <button
        class="px-2 py-0.5 rounded bg-zinc-100 dark:bg-zinc-800 text-[11px] inline-flex items-center gap-0.5"
        onclick={() => (menuOpen = !menuOpen)}
        title="more quick filters">
        +{overflow.length}
        <Icon name="chevron-down" size={12} />
      </button>
      {#if menuOpen}
        <div
          class="absolute right-0 top-full mt-1 w-56 rounded shadow-lg bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 z-30 max-h-64 overflow-y-auto"
          role="menu"
          tabindex="-1"
          onclick={(e) => e.stopPropagation()}
          onkeydown={(e) => e.key === "Escape" && (menuOpen = false)}>
          {#each overflow as c}
            <div class="group flex items-center hover:bg-zinc-100 dark:hover:bg-zinc-800">
              <button
                class="flex-1 text-left px-2 py-1.5 text-[11px] min-w-0"
                onclick={() => { onApply(c.expr); menuOpen = false; }}>
                <div class="font-medium truncate">{c.label}</div>
                <code class="mono text-[10px] text-zinc-500 truncate block">{c.expr || "(no filter)"}</code>
              </button>
              <button
                class="opacity-0 group-hover:opacity-100 px-2 text-zinc-500 hover:text-red-600"
                onclick={() => remove(c.label)}
                title="remove">
                <Icon name="x" size={12} />
              </button>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
  <button
    class="shrink-0 inline-flex items-center gap-1 px-2 py-0.5 rounded bg-zinc-100 dark:bg-zinc-800 hover:bg-zinc-200 dark:hover:bg-zinc-700 text-[11px]"
    title="save current filter as a quick chip"
    onclick={saveCurrent}>
    <Icon name="plus" size={12} /> save
  </button>
</div>
