<script lang="ts">
  import { tick } from "svelte";
  import Icon from "./Icon.svelte";
  import type { Entry } from "./types";

  // Floating context menu shown on right-click of a log row. Positioned in
  // viewport coords; clamps to stay on-screen. Closes on any outside click,
  // Escape, or after a menu item runs its action.

  let {
    entry,
    sourceName,
    pinned,
    selected,
    selectionSize,
    x,
    y,
    onClose,
    onAddFilter,
    onReplaceFilter,
    onFilterOnly,
    onTogglePin,
    onCopyMsg,
    onCopyJSON,
    onSelectToggle,
    onClearSelection,
    onDiff,
    onOpenDetail,
  } = $props<{
    entry: Entry;
    sourceName: string;
    pinned: boolean;
    selected: boolean;
    selectionSize: number;
    x: number;
    y: number;
    onClose: () => void;
    onAddFilter: (clause: string) => void;
    onReplaceFilter: (clause: string) => void;
    onFilterOnly: (clause: string) => void;
    onTogglePin: () => void;
    onCopyMsg: () => void;
    onCopyJSON: () => void;
    onSelectToggle: () => void;
    onClearSelection: () => void;
    onDiff: () => void;
    onOpenDetail: () => void;
  }>();

  let el: HTMLDivElement | null = $state(null);
  let pos = $state({ x, y });

  $effect(() => {
    if (!el) return;
    const r = el.getBoundingClientRect();
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const pad = 6;
    let nx = x;
    let ny = y;
    if (nx + r.width + pad > vw) nx = vw - r.width - pad;
    if (ny + r.height + pad > vh) ny = vh - r.height - pad;
    pos = { x: Math.max(pad, nx), y: Math.max(pad, ny) };
  });

  // Focus the first menu item once positioned, so ArrowDown/ArrowUp navigate
  // the menu naturally and Enter activates the focused item.
  $effect(() => {
    if (!el) return;
    tick().then(() => {
      const first = el!.querySelector<HTMLButtonElement>("button");
      first?.focus();
    });
  });

  function onMenuKey(e: KeyboardEvent) {
    if (!el) return;
    if (e.key === "Escape") {
      e.preventDefault();
      onClose();
      return;
    }
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp" && e.key !== "Home" && e.key !== "End") return;
    e.preventDefault();
    const buttons = Array.from(el.querySelectorAll<HTMLButtonElement>("button:not([disabled])"));
    if (buttons.length === 0) return;
    const cur = buttons.indexOf(document.activeElement as HTMLButtonElement);
    let next = cur;
    if (e.key === "ArrowDown") next = cur < 0 ? 0 : (cur + 1) % buttons.length;
    else if (e.key === "ArrowUp") next = cur < 0 ? buttons.length - 1 : (cur - 1 + buttons.length) % buttons.length;
    else if (e.key === "Home") next = 0;
    else if (e.key === "End") next = buttons.length - 1;
    buttons[next]?.focus();
  }

  function quoteIfNeeded(v: string): string {
    return /[\s:()\[\]"\\*]/.test(v)
      ? `"${v.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`
      : v;
  }

  function fire(action: () => void) {
    action();
    onClose();
  }

  let traceID = $derived((entry.fields as any)?.trace_id);
</script>

<svelte:window
  onclick={onClose}
  oncontextmenu={(e) => { e.preventDefault(); onClose(); }}
  onkeydown={(e) => e.key === "Escape" && onClose()} />

<div
  bind:this={el}
  class="fixed z-50 min-w-[200px] rounded-md shadow-lg bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 text-xs py-1"
  style={`left:${pos.x}px;top:${pos.y}px`}
  role="menu"
  tabindex="-1"
  onclick={(e) => e.stopPropagation()}
  oncontextmenu={(e) => e.stopPropagation()}
  onkeydown={(e) => { e.stopPropagation(); onMenuKey(e); }}>

  <button class="w-full text-left px-3 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800 inline-flex items-center gap-2"
          onclick={() => fire(onOpenDetail)}>
    <Icon name="chevron-right" size={12} class="opacity-60" />
    Open detail
  </button>

  <div class="border-t border-zinc-200 dark:border-zinc-800 my-1"></div>

  {#snippet filterRow(clause: string, label: string, value?: string)}
    <div class="filter-row group flex items-stretch hover:bg-zinc-100 dark:hover:bg-zinc-800">
      <button class="flex-1 text-left px-3 py-1.5 inline-flex items-center gap-2"
              title="Add this clause to the current filter"
              onclick={() => fire(() => onAddFilter(clause))}>
        <Icon name="filter" size={12} class="opacity-60" />
        {label}{#if value}: <span class="mono truncate max-w-[140px]">{value}</span>{/if}
      </button>
      <button class="opacity-0 group-hover:opacity-100 px-2 text-zinc-500 hover:text-sky-600 dark:hover:text-sky-400"
              title="Replace working filter with this clause"
              onclick={() => fire(() => onReplaceFilter(clause))}>
        <Icon name="refresh" size={12} />
      </button>
      <button class="opacity-0 group-hover:opacity-100 px-2 text-zinc-500 hover:text-amber-600 dark:hover:text-amber-400"
              title="Filter only by this — clears working filter and disables pinned"
              onclick={() => fire(() => onFilterOnly(clause))}>
        <Icon name="star" size={12} />
      </button>
    </div>
  {/snippet}

  {@render filterRow(`source:${quoteIfNeeded(sourceName)}`, "Filter to", sourceName)}
  {#if entry.level}
    {@render filterRow(`level:${entry.level}`, "Filter level", entry.level)}
  {/if}
  {#if entry.service}
    {@render filterRow(`service:${quoteIfNeeded(entry.service ?? "")}`, "Filter service", entry.service)}
  {/if}
  {#if traceID}
    {@render filterRow(`trace_id:${quoteIfNeeded(String(traceID))}`, "Show this trace")}
  {/if}

  <div class="border-t border-zinc-200 dark:border-zinc-800 my-1"></div>

  <button class="w-full text-left px-3 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800 inline-flex items-center gap-2"
          onclick={() => fire(onCopyMsg)}>
    <Icon name="copy" size={12} class="opacity-60" />
    Copy message
  </button>
  <button class="w-full text-left px-3 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800 inline-flex items-center gap-2"
          onclick={() => fire(onCopyJSON)}>
    <Icon name="copy" size={12} class="opacity-60" />
    Copy as JSON
  </button>

  <div class="border-t border-zinc-200 dark:border-zinc-800 my-1"></div>

  <button class="w-full text-left px-3 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800 inline-flex items-center gap-2"
          onclick={() => fire(onTogglePin)}>
    <Icon name={pinned ? "star-filled" : "star"} size={12} class={pinned ? "text-amber-500" : "opacity-60"} />
    {pinned ? "Unpin row" : "Pin row"}
  </button>
  <button class="w-full text-left px-3 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800 inline-flex items-center gap-2"
          onclick={() => fire(onSelectToggle)}>
    <Icon name={selected ? "check" : "plus"} size={12} class="opacity-60" />
    {selected ? "Remove from selection" : "Add to selection"}
  </button>
  {#if selectionSize === 2}
    <button class="w-full text-left px-3 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800 inline-flex items-center gap-2"
            onclick={() => fire(onDiff)}>
      <Icon name="diff" size={12} class="opacity-60" />
      Diff selected (2)
    </button>
  {/if}
  {#if selectionSize > 0}
    <button class="w-full text-left px-3 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800 inline-flex items-center gap-2 text-zinc-500"
            onclick={() => fire(onClearSelection)}>
      <Icon name="x" size={12} class="opacity-60" />
      Clear selection ({selectionSize})
    </button>
  {/if}
</div>
