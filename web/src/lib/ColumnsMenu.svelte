<script lang="ts">
  import { tick } from "svelte";
  import Icon from "./Icon.svelte";
  import ColumnReorderList from "./ColumnReorderList.svelte";
  import { type Column, BUILTINS } from "./columns";

  let {
    columns,
    discoveredFields,
    hotColumns,
    onChange,
    onClose,
  } = $props<{
    columns: Column[];
    discoveredFields: Set<string>;
    hotColumns: string[];
    onChange: (cols: Column[]) => void;
    onClose: () => void;
  }>();

  let dialogEl: HTMLDivElement | null = $state(null);
  let customInput = $state("");
  let filterInput = $state("");
  $effect(() => { if (dialogEl) tick().then(() => dialogEl?.focus()); });

  function patch(cols: Column[]) {
    onChange(cols);
  }

  function setVisible(i: number, v: boolean) {
    const next = [...columns];
    next[i] = { ...next[i], visible: v };
    patch(next);
  }
  function remove(i: number) {
    if (columns[i].kind === "builtin") {
      // built-ins can be hidden, not removed
      setVisible(i, false);
      return;
    }
    const next = columns.filter((_: Column, j: number) => j !== i);
    patch(next);
  }
  function addField(path: string) {
    let id = path.trim();
    if (!id) return;
    if (!id.startsWith("@")) id = "@" + id;
    if (columns.find((c: Column) => c.id === id)) {
      // already present — ensure visible
      patch(columns.map((c: Column) => (c.id === id ? { ...c, visible: true } : c)));
      return;
    }
    const next = [...columns, { id, label: id.slice(1), kind: "field" as const, width: 120, visible: true }];
    patch(next);
  }

  // Discovered field suggestions = (discoveredFields ∪ hotColumns) minus already-added,
  // filtered by the optional search input.
  let candidates = $derived.by(() => {
    const all = new Set<string>();
    for (const f of discoveredFields) all.add(f);
    for (const h of hotColumns) if (!(h in BUILTINS)) all.add(h);
    const have = new Set(columns.map((c: Column) => (c.id.startsWith("@") ? c.id.slice(1) : c.id)));
    const out: string[] = [];
    for (const f of all) {
      if (have.has(f)) continue;
      if (filterInput && !f.toLowerCase().includes(filterInput.toLowerCase())) continue;
      out.push(f);
    }
    out.sort();
    return out;
  });
</script>

<div
  class="fixed inset-0 bg-black/40 z-40 flex items-center justify-center"
  role="button"
  tabindex="-1"
  onclick={onClose}
  onkeydown={(e) => e.key === "Escape" && onClose()}>
  <div
    bind:this={dialogEl}
    class="bg-white dark:bg-zinc-900 rounded-lg shadow-xl w-[560px] max-h-[85vh] flex flex-col text-sm outline-none"
    role="dialog"
    tabindex="-1"
    onclick={(e) => e.stopPropagation()}
    onkeydown={(e) => e.stopPropagation()}>

    <header class="flex items-center justify-between px-4 py-2.5 border-b border-zinc-200 dark:border-zinc-800">
      <h2 class="font-semibold inline-flex items-center gap-2">
        <Icon name="columns" size={14} /> Columns
      </h2>
      <button class="text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100"
              onclick={onClose}
              aria-label="close">
        <Icon name="x" size={16} />
      </button>
    </header>

    <div class="flex-1 overflow-y-auto px-4 py-3 space-y-5">
      <p class="text-[10px] text-zinc-500">Column widths are saved per device; visibility and order follow your profile.</p>
      <section>
        <h3 class="text-[10px] uppercase tracking-wider text-zinc-500 font-semibold mb-2">Active</h3>
        <ColumnReorderList {columns} onChange={patch} onRemove={remove} />
      </section>

      <section>
        <div class="flex items-center justify-between mb-2">
          <h3 class="text-[10px] uppercase tracking-wider text-zinc-500 font-semibold">Add field column</h3>
          {#if candidates.length > 0}
            <input
              type="text"
              placeholder="search…"
              class="px-2 py-0.5 rounded bg-zinc-100 dark:bg-zinc-800 text-[11px] outline-none"
              bind:value={filterInput} />
          {/if}
        </div>
        <form
          class="flex gap-1 mb-2"
          onsubmit={(e) => { e.preventDefault(); addField(customInput); customInput = ""; }}>
          <input
            type="text"
            placeholder="dotted path, e.g. user.id or @request.id"
            class="flex-1 px-2 py-1 rounded bg-zinc-100 dark:bg-zinc-800 text-xs mono outline-none"
            bind:value={customInput} />
          <button type="submit"
                  class="px-2 py-1 rounded bg-sky-600 text-white text-xs hover:bg-sky-700">add</button>
        </form>
        {#if candidates.length === 0}
          <p class="text-[11px] text-zinc-500">No fields seen yet — once logs arrive, discovered JSON paths will show up here.</p>
        {:else}
          <ul class="grid grid-cols-2 gap-1 max-h-48 overflow-y-auto">
            {#each candidates as f}
              <li>
                <button
                  type="button"
                  class="w-full text-left px-2 py-1 rounded bg-zinc-50 dark:bg-zinc-800/60 hover:bg-sky-50 dark:hover:bg-sky-950/40 text-[11px] mono truncate"
                  title={`add @${f}`}
                  onclick={() => addField(f)}>
                  + @{f}
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    </div>
  </div>
</div>
