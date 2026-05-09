<script lang="ts">
  import type { SourceRef } from "./types";
  let {
    initialName = "",
    initialFilter = "",
    initialColumns = [],
    currentSources = [],
    onClose,
    onSaved,
  } = $props<{
    initialName?: string;
    initialFilter?: string;
    initialColumns?: string[];
    currentSources?: SourceRef[];
    onClose: () => void;
    onSaved: (name: string, path: string) => void;
  }>();

  let name = $state(initialName);
  let filter = $state(initialFilter);
  let dest = $state<"user" | "repo">("user");
  // Default OFF: profile == filter+columns is the historical mental model;
  // bundling sources changes runtime behaviour on activation, which the
  // user should opt into explicitly.
  let includeSources = $state(false);
  let saving = $state(false);
  let error = $state("");

  // Stdin sources can't be replayed at activation, so they're never
  // included even when the box is checked.
  let bundledSources = $derived(
    includeSources ? currentSources.filter((s: SourceRef) => s.kind !== "stdin") : [],
  );

  async function save() {
    if (!name.trim()) {
      error = "name is required";
      return;
    }
    saving = true;
    error = "";
    try {
      const r = await fetch("/api/profiles", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: name.trim(),
          filter,
          columns: initialColumns,
          collapsed_fields: [],
          sources: bundledSources,
          destination: dest,
        }),
      });
      const body = await r.json().catch(() => ({}));
      if (!r.ok) {
        error = body?.error ?? `HTTP ${r.status}`;
        return;
      }
      onSaved(name.trim(), body.path ?? "");
      onClose();
    } catch (e: any) {
      error = e?.message ?? "save failed";
    } finally {
      saving = false;
    }
  }
</script>

<div
  class="fixed inset-0 bg-black/40 z-40 flex items-center justify-center"
  role="button"
  tabindex="-1"
  onclick={onClose}
  onkeydown={(e) => e.key === "Escape" && onClose()}>
  <div
    class="bg-white dark:bg-zinc-900 rounded-lg shadow-xl p-5 w-[480px] text-sm"
    role="dialog"
    tabindex="-1"
    onclick={(e) => e.stopPropagation()}
    onkeydown={(e) => e.stopPropagation()}>
    <h2 class="font-semibold mb-3">Save profile</h2>
    <div class="space-y-3">
      <label class="block">
        <span class="text-xs text-zinc-500">Name</span>
        <input
          class="w-full mt-0.5 px-2 py-1 rounded bg-zinc-100 dark:bg-zinc-800 border border-transparent focus:border-sky-500 outline-none"
          bind:value={name}
          placeholder="e.g. errors-only"
          autofocus />
      </label>
      <label class="block">
        <span class="text-xs text-zinc-500">Filter</span>
        <input
          class="w-full mt-0.5 px-2 py-1 rounded bg-zinc-100 dark:bg-zinc-800 mono border border-transparent focus:border-sky-500 outline-none"
          bind:value={filter} />
      </label>
      <label class="block">
        <span class="text-xs text-zinc-500 inline-flex items-center gap-2">
          <input type="checkbox" bind:checked={includeSources} />
          Include current sources
        </span>
        {#if includeSources}
          <div class="mt-1 px-2 py-1.5 rounded bg-zinc-50 dark:bg-zinc-800/50 text-[11px] mono space-y-0.5 max-h-24 overflow-y-auto">
            {#if bundledSources.length === 0}
              <span class="text-zinc-500 italic">No file/docker sources currently open.</span>
            {:else}
              {#each bundledSources as s}
                <div class="flex gap-2"><span class="text-zinc-500 w-12">{s.kind}</span><span class="truncate">{s.name}</span></div>
              {/each}
            {/if}
          </div>
          <p class="mt-1 text-[10px] text-zinc-500">Activating this profile will start these and stop ones unique to the previous profile. Stdin sources are excluded.</p>
        {/if}
      </label>
      <div>
        <span class="text-xs text-zinc-500">Save to</span>
        <div class="mt-1 flex gap-2">
          <button
            class="flex-1 px-2 py-1.5 rounded text-xs"
            class:bg-sky-600={dest === "user"}
            class:text-white={dest === "user"}
            class:bg-zinc-200={dest !== "user"}
            class:dark:bg-zinc-800={dest !== "user"}
            onclick={() => (dest = "user")}>
            User config<br><span class="text-[10px] opacity-70 mono">~/.zz/loggi/config.toml</span>
          </button>
          <button
            class="flex-1 px-2 py-1.5 rounded text-xs"
            class:bg-sky-600={dest === "repo"}
            class:text-white={dest === "repo"}
            class:bg-zinc-200={dest !== "repo"}
            class:dark:bg-zinc-800={dest !== "repo"}
            onclick={() => (dest = "repo")}>
            Repo config<br><span class="text-[10px] opacity-70 mono">REPO/.loggi/config.toml</span>
          </button>
        </div>
      </div>
      {#if error}
        <div class="text-red-500 text-xs">⚠ {error}</div>
      {/if}
    </div>
    <div class="mt-4 flex justify-end gap-2">
      <button
        class="px-3 py-1.5 rounded bg-zinc-200 dark:bg-zinc-800 text-sm"
        onclick={onClose}
        disabled={saving}>Cancel</button>
      <button
        class="px-3 py-1.5 rounded bg-sky-600 text-white text-sm hover:bg-sky-700 disabled:opacity-50"
        onclick={save}
        disabled={saving}>{saving ? "Saving…" : "Save"}</button>
    </div>
  </div>
</div>
