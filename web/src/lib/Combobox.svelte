<script lang="ts">
  import { tick } from "svelte";
  import Icon from "./Icon.svelte";

  // A searchable dropdown. Generic on item.value (string). Items can carry
  // a `hint` shown beneath the label (used e.g. to show a profile's filter
  // expression).
  //
  // Keyboard:
  //   ArrowDown / ArrowUp — move highlight
  //   Enter               — pick highlighted item, close
  //   Escape              — close without changing
  //   Tab                 — close (browser-default tab order)
  //   typing              — filters the visible items
  //
  // The trigger button always shows the selected item's label; clicking or
  // pressing Enter / Space / ArrowDown opens the panel and focuses the search.

  export type ComboItem = { value: string; label: string; hint?: string };

  let {
    items,
    value,
    placeholder = "Select…",
    searchPlaceholder = "Search…",
    title = "",
    onChange,
    align = "left",
    width = "auto",
  } = $props<{
    items: ComboItem[];
    value: string;
    placeholder?: string;
    searchPlaceholder?: string;
    title?: string;
    onChange: (v: string) => void;
    align?: "left" | "right";
    width?: string;
  }>();

  let open = $state(false);
  let query = $state("");
  let highlight = $state(0);
  let triggerEl: HTMLButtonElement | null = $state(null);
  let panelEl: HTMLDivElement | null = $state(null);
  let inputEl: HTMLInputElement | null = $state(null);
  // Fixed-positioned coords from the trigger's bounding rect, so the
  // panel escapes any ancestor overflow:hidden / stacking context.
  let panelPos = $state({ left: 0, top: 0, width: 0 });

  function place() {
    if (!triggerEl) return;
    const r = triggerEl.getBoundingClientRect();
    const minW = 220;
    const w = Math.max(r.width, minW);
    let left = r.left;
    if (align === "right") {
      left = r.right - w;
    }
    panelPos = { left, top: r.bottom + 4, width: w };
  }

  let selected = $derived(items.find((i: ComboItem) => i.value === value) ?? null);

  let filtered = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return items;
    return items.filter((i: ComboItem) =>
      i.label.toLowerCase().includes(q) ||
      (i.hint ?? "").toLowerCase().includes(q),
    );
  });

  $effect(() => {
    // Reset highlight when filtered set changes.
    void filtered.length;
    highlight = 0;
  });

  async function show() {
    place();
    open = true;
    await tick();
    inputEl?.focus();
    inputEl?.select();
  }
  function hide() {
    open = false;
    query = "";
    triggerEl?.focus();
  }
  function pick(it: ComboItem) {
    onChange(it.value);
    open = false;
    query = "";
    triggerEl?.focus();
  }
  function onTriggerKey(e: KeyboardEvent) {
    if (e.key === "ArrowDown" || e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      show();
    }
  }
  function onInputKey(e: KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      highlight = Math.min(filtered.length - 1, highlight + 1);
      scrollIntoView();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      highlight = Math.max(0, highlight - 1);
      scrollIntoView();
    } else if (e.key === "Enter") {
      e.preventDefault();
      const it = filtered[highlight];
      if (it) pick(it);
    } else if (e.key === "Escape") {
      e.preventDefault();
      hide();
    } else if (e.key === "Tab") {
      open = false;
    } else if (e.key === "Home") {
      e.preventDefault();
      highlight = 0;
      scrollIntoView();
    } else if (e.key === "End") {
      e.preventDefault();
      highlight = filtered.length - 1;
      scrollIntoView();
    }
  }
  function scrollIntoView() {
    requestAnimationFrame(() => {
      const li = panelEl?.querySelector(`[data-idx="${highlight}"]`) as HTMLElement | null;
      li?.scrollIntoView({ block: "nearest" });
    });
  }

  // Outside click to close.
  function onWinClick(e: MouseEvent) {
    if (!open) return;
    const t = e.target as Node | null;
    if (t && (panelEl?.contains(t) || triggerEl?.contains(t))) return;
    open = false;
    query = "";
  }
</script>

<svelte:window onclick={onWinClick} onresize={() => open && place()} onscroll={() => open && place()} />

<div class="relative inline-block" style={width !== "auto" ? `width:${width}` : ""}>
  <button
    type="button"
    bind:this={triggerEl}
    {title}
    class="px-2 py-1 rounded bg-zinc-100 dark:bg-zinc-800 text-sm border border-transparent hover:bg-zinc-200 dark:hover:bg-zinc-700 focus:border-sky-500 outline-none inline-flex items-center gap-1 max-w-full"
    onclick={() => (open ? hide() : show())}
    onkeydown={onTriggerKey}
    aria-haspopup="listbox"
    aria-expanded={open}>
    <span class="truncate flex-1 text-left">{selected?.label ?? placeholder}</span>
    <Icon name="chevron-down" size={12} class="opacity-60 shrink-0" />
  </button>

  {#if open}
    <div
      bind:this={panelEl}
      class="fixed z-50 max-w-[360px] rounded-md shadow-lg bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800"
      style={`left:${panelPos.left}px;top:${panelPos.top}px;width:${panelPos.width}px`}
      role="listbox">
      <div class="px-2 py-1.5 border-b border-zinc-200 dark:border-zinc-800 flex items-center gap-1.5">
        <Icon name="search" size={12} class="text-zinc-400" />
        <input
          bind:this={inputEl}
          class="flex-1 bg-transparent text-xs mono outline-none"
          placeholder={searchPlaceholder}
          bind:value={query}
          onkeydown={onInputKey} />
      </div>
      <ul class="max-h-64 overflow-y-auto py-1">
        {#each filtered as it, i}
          <li
            data-idx={i}
            role="option"
            aria-selected={value === it.value}
            class="px-2.5 py-1 cursor-pointer text-xs"
            class:bg-sky-100={i === highlight}
            class:dark:bg-sky-900={i === highlight}
            onmouseenter={() => (highlight = i)}
            onclick={() => pick(it)}>
            <div class="flex items-center gap-1.5">
              {#if value === it.value}
                <Icon name="check" size={12} class="text-sky-600 dark:text-sky-400 shrink-0" />
              {:else}
                <span class="w-3"></span>
              {/if}
              <span class="font-medium truncate">{it.label}</span>
            </div>
            {#if it.hint}
              <code class="mono text-[10px] text-zinc-500 truncate block ml-[18px]">{it.hint}</code>
            {/if}
          </li>
        {/each}
        {#if filtered.length === 0}
          <li class="px-3 py-2 text-xs text-zinc-500 italic">No matches</li>
        {/if}
      </ul>
    </div>
  {/if}
</div>
