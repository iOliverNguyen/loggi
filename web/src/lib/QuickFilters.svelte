<script lang="ts">
  import Icon from "./Icon.svelte";
  import {
    type QuickChip,
    QUICK_CHANGED,
    loadQuickChips,
    persistQuickChips,
    requestSaveQuick,
    resetWorkingChips,
    setChipPinned,
    setChipEnabled,
  } from "./quick-filters";
  import { dismissOnOutside } from "./dismissable";

  let { activeFilter, currentFilter, onApply } = $props<{
    activeFilter: string;
    currentFilter: string;
    onApply: (expr: string) => void;
  }>();

  let chips = $state<QuickChip[]>(loadQuickChips());
  let menuOpen = $state(false);
  let pinnedExpanded = $state(false);
  let overflowEl: HTMLDivElement | null = $state(null);
  let containerEl: HTMLDivElement | null = $state(null);
  let listEl: HTMLDivElement | null = $state(null);

  $effect(() => {
    if (!menuOpen) return;
    return dismissOnOutside(overflowEl, () => (menuOpen = false));
  });
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

  let pinnedChips = $derived(chips.filter((c) => c.pinned));
  let workingChips = $derived(chips.filter((c) => !c.pinned));

  function isActive(expr: string): boolean {
    return expr === activeFilter;
  }

  function saveCurrent() {
    requestSaveQuick(currentFilter);
  }

  function remove(label: string) {
    persistQuickChips(chips.filter((c) => c.label !== label));
    chips = loadQuickChips();
  }

  function toggleEnabled(label: string) {
    const c = chips.find((x) => x.label === label);
    setChipEnabled(label, !(c?.enabled !== false));
    chips = loadQuickChips();
  }

  function unpin(label: string) {
    setChipPinned(label, false);
    chips = loadQuickChips();
  }

  function pin(label: string) {
    setChipPinned(label, true);
    chips = loadQuickChips();
  }

  function pinFromOverflow(label: string) {
    pin(label);
  }

  function resetDefaults() {
    resetWorkingChips();
  }

  // Drag-to-reorder for working chips. Same pattern as ColumnHeader: a
  // pixel threshold gates the drag so a quick mousedown→up still acts
  // as a click (apply filter). After a real drag we set `dragJustEnded`
  // and the chip's click handler swallows the trailing click event.
  const DRAG_THRESHOLD_PX = 4;
  let dragLabel = $state<string | null>(null);
  let dropIdx = $state<number | null>(null);
  let dragOriginX = 0;
  let dragOriginY = 0;
  let dragJustEnded = false;

  function chipRects(): { label: string; left: number; right: number }[] {
    if (!listEl) return [];
    const cells = listEl.querySelectorAll<HTMLElement>("[data-chip-label]");
    const out: { label: string; left: number; right: number }[] = [];
    for (const el of cells) {
      if (el.offsetParent === null) continue;
      const label = el.dataset.chipLabel!;
      const r = el.getBoundingClientRect();
      out.push({ label, left: r.left, right: r.right });
    }
    return out;
  }

  function onChipPointerDown(e: PointerEvent, label: string) {
    if (e.button !== 0) return;
    dragLabel = label;
    dragOriginX = e.clientX;
    dragOriginY = e.clientY;
    dropIdx = null;
    window.addEventListener("pointermove", onDragMove);
    window.addEventListener("pointerup", onDragUp, { once: true });
  }

  function onDragMove(e: PointerEvent) {
    if (dragLabel == null) return;
    const dx = e.clientX - dragOriginX;
    const dy = e.clientY - dragOriginY;
    if (dropIdx == null && Math.hypot(dx, dy) < DRAG_THRESHOLD_PX) return;
    const rects = chipRects();
    if (rects.length === 0) return;
    let chosen: { label: string; before: boolean } | null = null;
    for (const r of rects) {
      if (e.clientX < r.left) { chosen = { label: r.label, before: true }; break; }
      if (e.clientX <= r.right) {
        const mid = (r.left + r.right) / 2;
        chosen = { label: r.label, before: e.clientX < mid };
        break;
      }
    }
    if (!chosen) {
      const last = rects[rects.length - 1];
      chosen = { label: last.label, before: false };
    }
    let target = workingChips.findIndex((c: QuickChip) => c.label === chosen!.label);
    if (!chosen.before) target += 1;
    const from = workingChips.findIndex((c: QuickChip) => c.label === dragLabel);
    if (from >= 0 && from < target) target -= 1;
    dropIdx = Math.max(0, Math.min(workingChips.length - 1, target));
  }

  function onDragUp() {
    window.removeEventListener("pointermove", onDragMove);
    const dragged = dropIdx != null;
    if (dragLabel != null && dropIdx != null) {
      const all = loadQuickChips();
      const workingOrder = all.filter((c) => !c.pinned).map((c) => c.label);
      const from = workingOrder.indexOf(dragLabel);
      if (from >= 0 && from !== dropIdx) {
        const [moved] = workingOrder.splice(from, 1);
        workingOrder.splice(dropIdx, 0, moved);
        const byLabel = new Map(all.map((c) => [c.label, c]));
        const next: QuickChip[] = [];
        let wi = 0;
        for (const c of all) {
          if (c.pinned) next.push(c);
          else next.push(byLabel.get(workingOrder[wi++])!);
        }
        persistQuickChips(next);
        chips = loadQuickChips();
      }
    }
    dragLabel = null;
    dropIdx = null;
    if (dragged) dragJustEnded = true;
  }

  function onChipClick(e: MouseEvent, expr: string) {
    if (dragJustEnded) {
      dragJustEnded = false;
      e.preventDefault();
      return;
    }
    onApply(expr);
  }

  function measure() {
    if (!listEl || !containerEl) return;
    const containerW = containerEl.clientWidth;
    // Reserve space for the leading label, pinned section, and trailing buttons.
    const budget = Math.max(0, containerW - 320);
    const items = Array.from(listEl.children) as HTMLElement[];
    let used = 0;
    let count = 0;
    for (const el of items) {
      const w = el.offsetWidth + 6;
      if (used + w > budget && count > 0) break;
      used += w;
      count++;
    }
    visibleCount = Math.max(1, Math.min(count, items.length));
  }

  function measureWithReveal() {
    if (!listEl) return;
    listEl.classList.add("measuring");
    void listEl.offsetWidth;
    measure();
    listEl.classList.remove("measuring");
  }

  $effect(() => {
    if (!containerEl) return;
    const ro = new ResizeObserver(() => measureWithReveal());
    ro.observe(containerEl);
    measureWithReveal();
    return () => ro.disconnect();
  });

  $effect(() => {
    void chips.length;
    queueMicrotask(measureWithReveal);
  });

  let overflow = $derived(workingChips.slice(visibleCount));
</script>

<div
  bind:this={containerEl}
  class="px-4 py-1.5 border-b border-zinc-200 dark:border-zinc-800 flex items-center gap-2 text-xs"
  onkeydown={(e) => {
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
  {#if pinnedChips.length > 0}
    <button
      class="shrink-0 inline-flex items-center gap-1 px-2 py-0.5 rounded text-[11px] mono whitespace-nowrap bg-amber-500/15 text-amber-700 dark:text-amber-300 hover:bg-amber-500/25"
      title={pinnedExpanded ? "Collapse pinned filters" : "Expand pinned filters"}
      aria-expanded={pinnedExpanded}
      onclick={() => (pinnedExpanded = !pinnedExpanded)}>
      <Icon name="pin" size={10} />
      {pinnedChips.length}
      <Icon name={pinnedExpanded ? "chevron-down" : "chevron-right"} size={10} />
    </button>
    {#if pinnedExpanded}
      <div class="flex items-center gap-1.5 shrink-0">
        {#each pinnedChips as c, i (c.label)}
          {@const enabled = c.enabled !== false}
          <span class="chip-wrap group relative inline-flex items-center">
            <button
              class={`px-2 py-0.5 rounded text-[11px] mono whitespace-nowrap transition-colors ${enabled ? "bg-amber-500 text-white" : "bg-amber-500/20 text-amber-700 dark:text-amber-300"}`}
              title={`${c.expr || "no filter"} — click to ${enabled ? "disable" : "enable"}${i < 9 ? ` (Shift+${i + 1})` : ""}`}
              onclick={() => toggleEnabled(c.label)}>
              {c.label}
            </button>
            <span class="chip-actions opacity-0 group-hover:opacity-100 focus-within:opacity-100 absolute -top-1.5 -right-1 inline-flex items-center gap-px">
              <button
                class="w-3.5 h-3.5 rounded-full bg-zinc-700 dark:bg-zinc-200 text-white dark:text-zinc-900 flex items-center justify-center"
                title="unpin"
                aria-label="unpin"
                onclick={() => unpin(c.label)}>
                <Icon name="pin" size={9} />
              </button>
              <button
                class="w-3.5 h-3.5 rounded-full bg-zinc-700 dark:bg-zinc-200 text-white dark:text-zinc-900 text-[9px] flex items-center justify-center"
                title="remove"
                aria-label="remove"
                onclick={() => remove(c.label)}>×</button>
            </span>
          </span>
        {/each}
      </div>
    {/if}
    <span class="shrink-0 w-px h-4 bg-zinc-300 dark:bg-zinc-700"></span>
  {/if}
  <div bind:this={listEl} class="quick-list flex items-center gap-1.5 min-w-0 flex-1 overflow-x-clip overflow-y-visible">
    {#if workingChips.length === 0}
      <button
        class="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[11px] mono text-zinc-500 hover:text-sky-600 dark:hover:text-sky-400 border border-dashed border-zinc-300 dark:border-zinc-700 hover:border-sky-400"
        title="Restore the default chip set (all / info+ / warn+ / error+)"
        onclick={resetDefaults}>
        <Icon name="refresh" size={10} />
        reset to defaults
      </button>
    {/if}
    {#each workingChips as c, i (c.label)}
      <span
        data-chip-label={c.label}
        class="chip-wrap group relative inline-flex items-center"
        class:chip-hidden={i >= visibleCount}
        class:opacity-50={dragLabel === c.label}
        class:ring-2={dropIdx === i && dragLabel && dragLabel !== c.label}
        class:ring-sky-400={dropIdx === i && dragLabel && dragLabel !== c.label}
        class:rounded={dropIdx === i && dragLabel && dragLabel !== c.label}>
        <button
          data-chip
          class="px-2 py-0.5 rounded text-[11px] mono whitespace-nowrap transition-colors select-none cursor-grab active:cursor-grabbing"
          class:bg-sky-600={isActive(c.expr)}
          class:text-white={isActive(c.expr)}
          class:bg-zinc-100={!isActive(c.expr)}
          class:dark:bg-zinc-800={!isActive(c.expr)}
          class:hover:bg-zinc-200={!isActive(c.expr)}
          class:dark:hover:bg-zinc-700={!isActive(c.expr)}
          title={c.expr || "no filter"}
          onpointerdown={(e) => onChipPointerDown(e, c.label)}
          onclick={(e) => onChipClick(e, c.expr)}>{c.label}</button>
        <span class="chip-actions opacity-0 group-hover:opacity-100 focus-within:opacity-100 absolute -top-1.5 -right-1 inline-flex items-center gap-px">
          <button
            class="w-3.5 h-3.5 rounded-full bg-zinc-700 dark:bg-zinc-200 text-white dark:text-zinc-900 flex items-center justify-center"
            title="pin"
            aria-label="pin"
            onclick={() => pin(c.label)}>
            <Icon name="pin" size={9} />
          </button>
          <button
            class="w-3.5 h-3.5 rounded-full bg-zinc-700 dark:bg-zinc-200 text-white dark:text-zinc-900 text-[9px] flex items-center justify-center"
            title="remove"
            aria-label="remove"
            onclick={() => remove(c.label)}>×</button>
        </span>
      </span>
    {/each}
  </div>
  {#if overflow.length > 0}
    <div class="relative shrink-0" bind:this={overflowEl}>
      <button
        class="px-2 py-0.5 rounded bg-zinc-100 dark:bg-zinc-800 text-[11px] inline-flex items-center gap-0.5"
        onclick={() => (menuOpen = !menuOpen)}
        title="more quick filters">
        +{overflow.length}
        <Icon name="chevron-down" size={12} />
      </button>
      {#if menuOpen}
        <div
          class="absolute right-0 top-full mt-1 w-64 rounded shadow-lg bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 z-30 max-h-72 overflow-y-auto"
          role="menu"
          tabindex="-1"
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
                class="opacity-0 group-hover:opacity-100 px-2 text-zinc-500 hover:text-amber-500"
                onclick={() => { pinFromOverflow(c.label); menuOpen = false; }}
                title="pin">
                <Icon name="pin" size={12} />
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

<style>
  .chip-hidden {
    display: none;
  }
  .quick-list.measuring .chip-hidden {
    display: inline-flex;
    visibility: hidden;
  }
</style>
