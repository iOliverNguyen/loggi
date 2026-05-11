<script lang="ts">
  import type { Column } from "./columns";
  import { BUILTINS } from "./columns";

  let { columns, showTimestamps, onChange } = $props<{
    columns: Column[];
    showTimestamps: boolean;
    onChange?: (next: Column[]) => void;
  }>();

  function labelFor(c: Column): string {
    if (c.kind === "builtin" && BUILTINS[c.id]) return BUILTINS[c.id].label;
    return c.label || (c.kind === "field" ? c.id.replace(/^@/, "") : c.id);
  }

  // Drag-to-reorder state. dragId is the source column id; dropIdx is the
  // index in `columns` where the source would land on pointerup, or null
  // when the gesture hasn't crossed the activation threshold yet.
  const DRAG_THRESHOLD_PX = 4;
  let dragId = $state<string | null>(null);
  let dropIdx = $state<number | null>(null);
  let dragOriginX = 0;
  let dragOriginY = 0;
  let rowEl: HTMLDivElement | null = $state(null);

  // Right-edge resize state. activeResize holds the column id being resized
  // and the live width during pointermove so we don't fight the upstream
  // `columns` prop until pointerup.
  let resizeId = $state<string | null>(null);
  let resizeLiveWidth = $state(0);
  let resizeStartX = 0;
  let resizeStartW = 0;

  // ts is intentionally allowed to participate; the render is conditional on
  // showTimestamps but the order is free. msg's resize commits a fixed width
  // (collapsing its flex behavior), matching the Columns menu's numeric input.

  function indexOf(id: string): number {
    return columns.findIndex((c: Column) => c.id === id);
  }

  function visibleColumnRects(): { id: string; left: number; right: number }[] {
    if (!rowEl) return [];
    const cells = rowEl.querySelectorAll<HTMLElement>("[data-col-id]");
    const out: { id: string; left: number; right: number }[] = [];
    for (const el of cells) {
      const id = el.dataset.colId!;
      const r = el.getBoundingClientRect();
      out.push({ id, left: r.left, right: r.right });
    }
    return out;
  }

  function onHeaderPointerDown(e: PointerEvent, id: string) {
    if (e.button !== 0) return;
    // Resize-handle clicks set their own listeners and stop propagation, so
    // this path only fires on the cell body.
    dragId = id;
    dragOriginX = e.clientX;
    dragOriginY = e.clientY;
    dropIdx = null;
    window.addEventListener("pointermove", onDragMove);
    window.addEventListener("pointerup", onDragUp, { once: true });
  }

  function onDragMove(e: PointerEvent) {
    if (dragId == null) return;
    const dx = e.clientX - dragOriginX;
    const dy = e.clientY - dragOriginY;
    if (dropIdx == null && Math.hypot(dx, dy) < DRAG_THRESHOLD_PX) return;

    const rects = visibleColumnRects();
    if (rects.length === 0) return;
    // Find the visible cell whose midpoint is nearest the cursor. Drop
    // *before* if cursor is on its left half, *after* otherwise.
    let chosen: { id: string; before: boolean } | null = null;
    for (const r of rects) {
      if (e.clientX < r.left) {
        chosen = { id: r.id, before: true };
        break;
      }
      if (e.clientX <= r.right) {
        const mid = (r.left + r.right) / 2;
        chosen = { id: r.id, before: e.clientX < mid };
        break;
      }
    }
    if (!chosen) {
      const last = rects[rects.length - 1];
      chosen = { id: last.id, before: false };
    }
    let target = indexOf(chosen.id);
    if (!chosen.before) target += 1;
    // Adjust for the removal of the source: if source is before target,
    // dropping into `target` lands at `target - 1` after the splice.
    const from = indexOf(dragId);
    if (from < target) target -= 1;
    dropIdx = Math.max(0, Math.min(columns.length - 1, target));
  }

  function onDragUp() {
    window.removeEventListener("pointermove", onDragMove);
    if (dragId != null && dropIdx != null) {
      const from = indexOf(dragId);
      if (from !== dropIdx && from >= 0) {
        const next = [...columns];
        const [moved] = next.splice(from, 1);
        next.splice(dropIdx, 0, moved);
        onChange?.(next);
      }
    }
    dragId = null;
    dropIdx = null;
  }

  // Resize handle.
  function onResizeDown(e: PointerEvent, c: Column) {
    if (e.button !== 0) return;
    e.stopPropagation();
    e.preventDefault();
    resizeId = c.id;
    resizeStartX = e.clientX;
    resizeStartW = c.width || 120;
    resizeLiveWidth = resizeStartW;
    window.addEventListener("pointermove", onResizeMove);
    window.addEventListener("pointerup", onResizeUp, { once: true });
  }

  function onResizeMove(e: PointerEvent) {
    if (resizeId == null) return;
    const w = Math.max(24, Math.round(resizeStartW + (e.clientX - resizeStartX)));
    resizeLiveWidth = w;
    const next = columns.map((c: Column) => (c.id === resizeId ? { ...c, width: w } : c));
    onChange?.(next);
  }

  function onResizeUp() {
    window.removeEventListener("pointermove", onResizeMove);
    resizeId = null;
  }

  function onResizeDblClick(e: MouseEvent, c: Column) {
    e.stopPropagation();
    const def = c.id in BUILTINS ? BUILTINS[c.id].width : 120;
    const next = columns.map((x: Column) => (x.id === c.id ? { ...x, width: def } : x));
    onChange?.(next);
  }

  function widthFor(c: Column): number {
    return resizeId === c.id ? resizeLiveWidth : c.width;
  }

  function shouldRender(c: Column): boolean {
    if (!c.visible) return false;
    if (c.id === "ts" && !showTimestamps) return false;
    return true;
  }
</script>

<div
  bind:this={rowEl}
  class="relative pl-4 pr-3 flex gap-3 py-1 text-[10px] uppercase tracking-wide text-zinc-500 dark:text-zinc-400 select-none">
  {#each columns as c, i (c.id)}
    {#if shouldRender(c)}
      {@const w = widthFor(c)}
      {@const isMsg = c.id === "msg"}
      {@const isFlex = isMsg && w === 0}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        data-col-id={c.id}
        class="relative group/col truncate"
        class:flex-1={isFlex}
        class:shrink-0={!isFlex}
        class:cursor-grab={!resizeId}
        class:opacity-50={dragId === c.id}
        class:ring-2={dropIdx === i && dragId && dragId !== c.id}
        class:ring-sky-400={dropIdx === i && dragId && dragId !== c.id}
        class:rounded={dropIdx === i && dragId && dragId !== c.id}
        style={isFlex ? "" : `width:${w}px;flex:none`}
        title="drag to reorder · drag right edge to resize"
        onpointerdown={(e) => onHeaderPointerDown(e, c.id)}>
        <span class="block truncate pr-1">{labelFor(c)}</span>
        {#if onChange && !isFlex}
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div
            class="absolute top-0 right-0 h-full w-1.5 cursor-col-resize bg-transparent hover:bg-sky-400/60"
            class:bg-sky-500={resizeId === c.id}
            onpointerdown={(e) => onResizeDown(e, c)}
            ondblclick={(e) => onResizeDblClick(e, c)}
            title="drag to resize · double-click to reset"
            aria-hidden="true">
          </div>
        {/if}
      </div>
    {/if}
  {/each}
</div>
