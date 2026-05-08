<script lang="ts">
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
  onkeydown={(e) => e.stopPropagation()}>

  <button class="w-full text-left px-3 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800 inline-flex items-center gap-2"
          onclick={() => fire(onOpenDetail)}>
    <Icon name="chevron-right" size={12} class="opacity-60" />
    Open detail
  </button>

  <div class="border-t border-zinc-200 dark:border-zinc-800 my-1"></div>

  <button class="w-full text-left px-3 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800 inline-flex items-center gap-2"
          onclick={() => fire(() => onAddFilter(`source:${quoteIfNeeded(sourceName)}`))}>
    <Icon name="filter" size={12} class="opacity-60" />
    Filter to <span class="mono truncate max-w-[140px]">{sourceName}</span>
  </button>
  {#if entry.level}
    <button class="w-full text-left px-3 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800 inline-flex items-center gap-2"
            onclick={() => fire(() => onAddFilter(`level:${entry.level}`))}>
      <Icon name="filter" size={12} class="opacity-60" />
      Filter level: {entry.level}
    </button>
  {/if}
  {#if entry.service}
    <button class="w-full text-left px-3 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800 inline-flex items-center gap-2"
            onclick={() => fire(() => onAddFilter(`service:${quoteIfNeeded(entry.service ?? "")}`))}>
      <Icon name="filter" size={12} class="opacity-60" />
      Filter service: {entry.service}
    </button>
  {/if}
  {#if traceID}
    <button class="w-full text-left px-3 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800 inline-flex items-center gap-2"
            onclick={() => fire(() => onAddFilter(`trace_id:${quoteIfNeeded(String(traceID))}`))}>
      <Icon name="filter" size={12} class="opacity-60" />
      Show this trace
    </button>
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
