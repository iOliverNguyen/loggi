<script lang="ts">
  import Icon from "./Icon.svelte";
  import type { Column } from "./columns";

  let { columns, onChange, onRemove } = $props<{
    columns: Column[];
    onChange: (cols: Column[]) => void;
    onRemove?: (i: number) => void;
  }>();

  let dragIdx = $state<number | null>(null);
  let overIdx = $state<number | null>(null);

  function reorder(from: number, to: number) {
    if (from === to || from < 0 || to < 0) return;
    const next = [...columns];
    const [moved] = next.splice(from, 1);
    next.splice(to, 0, moved);
    onChange(next);
  }

  function setVisible(i: number, v: boolean) {
    const next = [...columns];
    next[i] = { ...next[i], visible: v };
    onChange(next);
  }
  function setWidth(i: number, w: number) {
    const next = [...columns];
    next[i] = { ...next[i], width: w };
    onChange(next);
  }

  // Alt+ArrowUp/Down keyboard reorder for a11y. Focus must be on the row.
  function onRowKey(e: KeyboardEvent, i: number) {
    if (!e.altKey) return;
    if (e.key === "ArrowUp" && i > 0) {
      e.preventDefault();
      reorder(i, i - 1);
    } else if (e.key === "ArrowDown" && i < columns.length - 1) {
      e.preventDefault();
      reorder(i, i + 1);
    }
  }
</script>

<ul class="space-y-1">
  {#each columns as c, i (c.id)}
    <!-- svelte-ignore a11y_no_noninteractive_tabindex a11y_no_noninteractive_element_interactions -->
    <li
      draggable="true"
      tabindex="0"
      class="flex items-center gap-1 text-xs px-1 py-0.5 rounded outline-none focus:ring-1 focus:ring-sky-500"
      class:bg-sky-50={overIdx === i && dragIdx !== null && dragIdx !== i}
      class:dark:bg-sky-950={overIdx === i && dragIdx !== null && dragIdx !== i}
      ondragstart={(e) => {
        dragIdx = i;
        e.dataTransfer?.setData("text/plain", String(i));
        if (e.dataTransfer) e.dataTransfer.effectAllowed = "move";
      }}
      ondragover={(e) => { e.preventDefault(); overIdx = i; }}
      ondragleave={() => { if (overIdx === i) overIdx = null; }}
      ondrop={(e) => {
        e.preventDefault();
        if (dragIdx !== null) reorder(dragIdx, i);
        dragIdx = null;
        overIdx = null;
      }}
      ondragend={() => { dragIdx = null; overIdx = null; }}
      onkeydown={(e) => onRowKey(e, i)}>
      <span class="cursor-grab text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 px-0.5"
            title="drag to reorder (Alt+↑/↓)">
        <Icon name="grip" size={12} />
      </span>
      <input
        type="checkbox"
        checked={c.visible}
        onchange={(e) => setVisible(i, (e.currentTarget as HTMLInputElement).checked)}
        aria-label={`toggle ${c.label}`} />
      <span class="flex-1 truncate" class:opacity-50={!c.visible}>
        <span class="font-mono text-[11px]">{c.id}</span>
        {#if c.label !== c.id}<span class="text-zinc-500"> · {c.label}</span>{/if}
      </span>
      <input
        type="number"
        min="0"
        max="800"
        step="8"
        class="w-16 px-1.5 py-0.5 rounded bg-zinc-100 dark:bg-zinc-800 text-[11px] mono"
        value={c.width}
        title={c.width === 0 ? "flex (fills remaining space)" : `${c.width}px`}
        onchange={(e) => setWidth(i, Number((e.currentTarget as HTMLInputElement).value) || 0)} />
      {#if onRemove}
        <button
          class="p-1 text-zinc-500 hover:text-rose-500"
          title={c.kind === "builtin" ? "hide" : "remove"}
          onclick={() => onRemove?.(i)}
          aria-label="remove">
          <Icon name="x" size={12} />
        </button>
      {/if}
    </li>
  {/each}
</ul>
