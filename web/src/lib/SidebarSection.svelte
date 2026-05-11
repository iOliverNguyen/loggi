<script lang="ts">
  import type { Snippet } from "svelte";
  import { untrack } from "svelte";
  import Icon from "./Icon.svelte";

  let {
    id,
    label,
    defaultOpen = true,
    count,
    children,
    headerExtra,
    open = $bindable(),
  } = $props<{
    id: string;
    label: string;
    defaultOpen?: boolean;
    count?: number | string;
    children: Snippet<[{ open: boolean }]>;
    headerExtra?: Snippet<[{ open: boolean }]>;
    open?: boolean;
  }>();

  const KEY = untrack(() => `loggi.sidebar.open.${id}`);
  if (open === undefined) {
    open = (localStorage.getItem(KEY) ?? (defaultOpen ? "1" : "0")) !== "0";
  }
  $effect(() => { try { localStorage.setItem(KEY, open ? "1" : "0"); } catch {} });
</script>

<section class="mb-3">
  <div class="flex items-center justify-between mb-1 gap-2">
    <button
      type="button"
      class="flex items-center gap-1 font-semibold text-zinc-700 dark:text-zinc-200 hover:text-sky-600 min-w-0"
      onclick={() => (open = !open)}
      aria-expanded={open}>
      <Icon name={open ? "chevron-down" : "chevron-right"} size={12} />
      <span class="truncate">{label}</span>
      {#if count !== undefined}
        <span class="text-[10px] text-zinc-400 font-normal">{count}</span>
      {/if}
    </button>
    {#if headerExtra}<div class="flex items-center gap-1 shrink-0">{@render headerExtra({ open })}</div>{/if}
  </div>
  {#if open}{@render children({ open })}{/if}
</section>
