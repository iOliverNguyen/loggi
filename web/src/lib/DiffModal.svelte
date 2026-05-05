<script lang="ts">
  import type { Entry } from "./types";

  let { entries: rows, onClose } = $props<{ entries: Entry[]; onClose: () => void }>();

  // Flatten an entry into [path, value] pairs for diffing.
  function flatten(e: Entry): Map<string, string> {
    const m = new Map<string, string>();
    m.set("ts", String(e.ts));
    m.set("source_id", String(e.source_id));
    m.set("level", e.level ?? "");
    m.set("service", e.service ?? "");
    m.set("msg", e.msg ?? "");
    walk(e.fields ?? {}, [], m);
    return m;
  }
  function walk(node: unknown, path: string[], out: Map<string, string>) {
    if (node === null || typeof node !== "object") {
      out.set(path.join("."), node === undefined ? "" : JSON.stringify(node));
      return;
    }
    if (Array.isArray(node)) {
      out.set(path.join("."), JSON.stringify(node));
      return;
    }
    for (const [k, v] of Object.entries(node)) {
      walk(v, [...path, k], out);
    }
  }

  let a = $derived(flatten(rows[0]));
  let b = $derived(flatten(rows[1]));
  let allKeys = $derived(
    Array.from(new Set([...a.keys(), ...b.keys()])).sort(),
  );
  let onlyDiffs = $state(true);
  let visibleKeys = $derived(
    onlyDiffs ? allKeys.filter((k) => a.get(k) !== b.get(k)) : allKeys,
  );
</script>

<div
  class="fixed inset-0 bg-black/40 z-40 flex items-center justify-center"
  role="button"
  tabindex="-1"
  onclick={onClose}
  onkeydown={(e) => e.key === "Escape" && onClose()}>
  <div
    class="bg-white dark:bg-zinc-900 rounded-lg shadow-xl w-[90vw] max-w-[1200px] max-h-[80vh] flex flex-col"
    role="dialog"
    tabindex="-1"
    onclick={(e) => e.stopPropagation()}
    onkeydown={(e) => e.stopPropagation()}>
    <div class="px-4 py-3 border-b border-zinc-200 dark:border-zinc-800 flex items-center gap-3">
      <h2 class="font-semibold">Diff rows #{rows[0].seq} ↔ #{rows[1].seq}</h2>
      <label class="text-xs flex items-center gap-1 text-zinc-500 ml-auto">
        <input type="checkbox" bind:checked={onlyDiffs} /> only differences
      </label>
      <button class="text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 px-2"
              onclick={onClose}>×</button>
    </div>
    <div class="flex-1 overflow-y-auto p-3 mono text-[11px]">
      <table class="w-full">
        <thead class="text-[10px] uppercase tracking-wider text-zinc-500">
          <tr>
            <th class="text-left p-1 w-1/4">field</th>
            <th class="text-left p-1 w-3/8">#{rows[0].seq}</th>
            <th class="text-left p-1 w-3/8">#{rows[1].seq}</th>
          </tr>
        </thead>
        <tbody>
          {#each visibleKeys as k}
            {@const va = a.get(k) ?? ""}
            {@const vb = b.get(k) ?? ""}
            {@const same = va === vb}
            <tr class="border-t border-zinc-100 dark:border-zinc-900 align-top"
                class:bg-amber-50={!same}
                class:dark:bg-amber-950={!same}>
              <td class="p-1 text-violet-700 dark:text-violet-300 break-all">{k}</td>
              <td class="p-1 break-all">{va}</td>
              <td class="p-1 break-all">{vb}</td>
            </tr>
          {/each}
          {#if visibleKeys.length === 0}
            <tr><td colspan="3" class="p-4 text-center text-zinc-500">no differences</td></tr>
          {/if}
        </tbody>
      </table>
    </div>
  </div>
</div>
