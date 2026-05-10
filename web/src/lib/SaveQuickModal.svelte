<script lang="ts">
  import { tick } from "svelte";
  import Icon from "./Icon.svelte";
  import { commitQuickChip, loadQuickChips } from "./quick-filters";

  let { expr, onClose } = $props<{
    expr: string;
    onClose: (saved: boolean) => void;
  }>();

  let dialogEl: HTMLDivElement | null = $state(null);
  let inputEl: HTMLInputElement | null = $state(null);
  let label = $state("");
  let pinned = $state(false);
  let error = $state("");

  $effect(() => {
    if (!dialogEl) return;
    tick().then(() => {
      dialogEl?.focus();
      inputEl?.focus();
      inputEl?.select();
    });
  });

  let exists = $derived(label.trim() !== "" && loadQuickChips().some((c) => c.label === label.trim()));

  function save() {
    const r = commitQuickChip(label, expr, pinned);
    if (!r.ok) {
      error = "name is required";
      return;
    }
    onClose(true);
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.preventDefault();
      onClose(false);
    } else if (e.key === "Enter") {
      e.preventDefault();
      save();
    }
  }
</script>

<div
  class="fixed inset-0 bg-black/40 z-40 flex items-center justify-center"
  role="button"
  tabindex="-1"
  onclick={() => onClose(false)}
  onkeydown={onKey}>
  <div
    bind:this={dialogEl}
    class="bg-white dark:bg-zinc-900 rounded-lg shadow-xl w-[420px] flex flex-col text-sm outline-none"
    role="dialog"
    tabindex="-1"
    onclick={(e) => e.stopPropagation()}
    onkeydown={(e) => { e.stopPropagation(); onKey(e); }}>
    <header class="flex items-center justify-between px-4 py-2.5 border-b border-zinc-200 dark:border-zinc-800">
      <h2 class="font-semibold">Save quick filter</h2>
      <button
        class="text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100"
        onclick={() => onClose(false)}
        aria-label="close">
        <Icon name="x" size={16} />
      </button>
    </header>

    <div class="px-4 py-3 space-y-3">
      <label class="block">
        <span class="text-xs text-zinc-500">Name</span>
        <input
          bind:this={inputEl}
          bind:value={label}
          class="w-full mt-0.5 px-2 py-1 rounded bg-zinc-100 dark:bg-zinc-800 border border-transparent focus:border-sky-500 outline-none"
          placeholder="e.g. errors-only" />
      </label>
      <div>
        <span class="text-xs text-zinc-500">Filter</span>
        <code class="mono text-[11px] block mt-0.5 px-2 py-1 rounded bg-zinc-100 dark:bg-zinc-800 break-all">{expr || "(no filter)"}</code>
      </div>
      <label class="flex items-start gap-2 cursor-pointer">
        <input type="checkbox" bind:checked={pinned} class="mt-0.5" />
        <span class="text-xs">
          <span class="font-medium">Pin</span>
          <span class="text-zinc-500"> — pinned chips AND on top of the working filter; toggle them on/off without retyping.</span>
        </span>
      </label>
      {#if exists}
        <div class="text-amber-600 dark:text-amber-400 text-xs">⚠ A chip named &quot;{label.trim()}&quot; already exists. Saving will replace it.</div>
      {/if}
      {#if error}
        <div class="text-red-500 text-xs">⚠ {error}</div>
      {/if}
    </div>

    <footer class="px-4 py-2.5 border-t border-zinc-200 dark:border-zinc-800 flex justify-end gap-2">
      <button
        class="px-3 py-1.5 rounded bg-zinc-200 dark:bg-zinc-800 text-xs"
        onclick={() => onClose(false)}>Cancel</button>
      <button
        class="px-3 py-1.5 rounded bg-sky-600 text-white text-xs hover:bg-sky-700 inline-flex items-center gap-1.5"
        onclick={save}>
        <Icon name="save" size={12} /> {exists ? "Replace" : "Save"}
      </button>
    </footer>
  </div>
</div>
