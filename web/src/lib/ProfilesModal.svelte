<script lang="ts">
  import { tick } from "svelte";
  import type { Profile } from "./types";
  import Icon from "./Icon.svelte";

  let dialogEl: HTMLDivElement | null = $state(null);
  $effect(() => { if (dialogEl) tick().then(() => dialogEl?.focus()); });

  let {
    profiles,
    activeProfile,
    currentFilter,
    onClose,
    onChanged,
    onActivate,
  } = $props<{
    profiles: Profile[];
    activeProfile: string;
    currentFilter: string;
    onClose: () => void;
    onChanged: () => Promise<void>;
    onActivate: (name: string) => void;
  }>();

  let mode = $state<"list" | "edit" | "new">("list");
  let editing = $state<{ originalName: string; name: string; filter: string; dest: "user" | "repo" } | null>(null);
  let saving = $state(false);
  let error = $state("");

  function startNew() {
    editing = { originalName: "", name: "", filter: currentFilter, dest: "user" };
    mode = "new";
    error = "";
  }
  function startEdit(p: Profile) {
    editing = { originalName: p.name, name: p.name, filter: p.filter ?? "", dest: "user" };
    mode = "edit";
    error = "";
  }
  // Duplicate: open the new-profile editor pre-filled with the source
  // profile's filter and a derived name. originalName stays empty so
  // the save path doesn't try to delete the source.
  function startDuplicate(p: Profile) {
    const existing = new Set(profiles.map((x: Profile) => x.name));
    let candidate = `${p.name} (copy)`;
    let n = 2;
    while (existing.has(candidate)) candidate = `${p.name} (copy ${n++})`;
    editing = { originalName: "", name: candidate, filter: p.filter ?? "", dest: "user" };
    mode = "new";
    error = "";
  }
  function cancelEdit() {
    editing = null;
    mode = "list";
    error = "";
  }

  async function save() {
    if (!editing) return;
    const trimmed = editing.name.trim();
    if (!trimmed) {
      error = "name is required";
      return;
    }
    saving = true;
    error = "";
    try {
      // If renaming, delete the old one after the new one saves successfully.
      // Preserve existing per-profile collapsed_fields/columns/sources on
      // edit; the modal only re-edits name + filter.
      const original = profiles.find((p: Profile) => p.name === editing!.originalName);
      const r = await fetch("/api/profiles", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: trimmed,
          filter: editing.filter,
          columns: original?.columns ?? [],
          collapsed_fields: original?.collapsed_fields ?? [],
          sources: original?.sources ?? [],
          destination: editing.dest,
        }),
      });
      const body = await r.json().catch(() => ({}));
      if (!r.ok) {
        error = body?.error ?? `HTTP ${r.status}`;
        return;
      }
      if (editing.originalName && editing.originalName !== trimmed) {
        await fetch(`/api/profiles?name=${encodeURIComponent(editing.originalName)}&destination=${editing.dest}`, {
          method: "DELETE",
        }).catch(() => {});
      }
      await onChanged();
      cancelEdit();
    } catch (e: any) {
      error = e?.message ?? "save failed";
    } finally {
      saving = false;
    }
  }

  async function del(p: Profile) {
    if (!confirm(`Delete profile "${p.name}"?`)) return;
    try {
      // Try user dest first, fall back to repo if 404.
      let r = await fetch(`/api/profiles?name=${encodeURIComponent(p.name)}&destination=user`, { method: "DELETE" });
      if (!r.ok) {
        r = await fetch(`/api/profiles?name=${encodeURIComponent(p.name)}&destination=repo`, { method: "DELETE" });
      }
      if (!r.ok) {
        const b = await r.json().catch(() => ({}));
        error = b?.error ?? `HTTP ${r.status}`;
        return;
      }
      await onChanged();
    } catch (e: any) {
      error = e?.message ?? "delete failed";
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
    bind:this={dialogEl}
    class="bg-white dark:bg-zinc-900 rounded-lg shadow-xl w-[560px] max-h-[80vh] flex flex-col text-sm outline-none"
    role="dialog"
    tabindex="-1"
    onclick={(e) => e.stopPropagation()}
    onkeydown={(e) => e.stopPropagation()}>

    <header class="flex items-center justify-between px-4 py-2.5 border-b border-zinc-200 dark:border-zinc-800">
      <h2 class="font-semibold">
        {mode === "list" ? "Profiles" : mode === "new" ? "New profile" : `Edit ${editing?.originalName ?? ""}`}
      </h2>
      <button
        class="text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100"
        onclick={onClose}
        aria-label="close">
        <Icon name="x" size={16} />
      </button>
    </header>

    {#if mode === "list"}
      <div class="flex-1 overflow-y-auto px-4 py-3">
        {#if profiles.length === 0}
          <p class="text-zinc-500 text-xs py-4 text-center">No profiles yet.</p>
        {:else}
          <ul class="space-y-1">
            {#each profiles as p}
              <li class="group flex items-center gap-2 px-2 py-1.5 rounded hover:bg-zinc-100 dark:hover:bg-zinc-800">
                <button
                  class="flex-1 text-left min-w-0"
                  onclick={() => onActivate(p.name)}
                  title="activate">
                  <div class="flex items-center gap-2">
                    <span class="font-medium truncate">{p.name}</span>
                    {#if p.name === activeProfile}
                      <span class="text-[10px] px-1.5 py-0.5 rounded bg-sky-600/15 text-sky-700 dark:text-sky-400">active</span>
                    {/if}
                  </div>
                  {#if p.filter}
                    <code class="mono text-[11px] text-zinc-500 truncate block">{p.filter}</code>
                  {:else}
                    <span class="text-[11px] text-zinc-400 italic">no filter</span>
                  {/if}
                </button>
                <button
                  class="opacity-0 group-hover:opacity-100 text-zinc-500 hover:text-sky-600 p-1"
                  onclick={() => startDuplicate(p)}
                  title="duplicate">
                  <Icon name="copy" size={14} />
                </button>
                <button
                  class="opacity-0 group-hover:opacity-100 text-zinc-500 hover:text-sky-600 p-1"
                  onclick={() => startEdit(p)}
                  title="edit">
                  <Icon name="edit" size={14} />
                </button>
                <button
                  class="opacity-0 group-hover:opacity-100 text-zinc-500 hover:text-red-600 p-1"
                  onclick={() => del(p)}
                  title="delete">
                  <Icon name="trash" size={14} />
                </button>
              </li>
            {/each}
          </ul>
        {/if}
        {#if error}
          <div class="text-red-500 text-xs mt-3">⚠ {error}</div>
        {/if}
      </div>
      <footer class="px-4 py-2.5 border-t border-zinc-200 dark:border-zinc-800 flex justify-between items-center gap-2">
        <span class="text-[11px] text-zinc-500 truncate" title={currentFilter}>
          {currentFilter ? `current: ${currentFilter}` : "current filter is empty"}
        </span>
        <div class="flex gap-2 shrink-0">
          {#if currentFilter.trim()}
            <button
              class="px-3 py-1.5 rounded bg-zinc-200 dark:bg-zinc-800 text-xs inline-flex items-center gap-1.5 hover:bg-zinc-300 dark:hover:bg-zinc-700"
              onclick={startNew}
              title="Save the current filter as a new profile">
              <Icon name="save" size={14} /> Save current as new
            </button>
          {/if}
          <button
            class="px-3 py-1.5 rounded bg-sky-600 text-white text-xs hover:bg-sky-700 inline-flex items-center gap-1.5"
            onclick={() => { editing = { originalName: "", name: "", filter: "", dest: "user" }; mode = "new"; error = ""; }}>
            <Icon name="plus" size={14} /> New profile
          </button>
        </div>
      </footer>
    {:else if editing}
      <div class="flex-1 overflow-y-auto px-4 py-3 space-y-3">
        <label class="block">
          <span class="text-xs text-zinc-500">Name</span>
          <input
            class="w-full mt-0.5 px-2 py-1 rounded bg-zinc-100 dark:bg-zinc-800 border border-transparent focus:border-sky-500 outline-none"
            bind:value={editing.name}
            placeholder="e.g. errors-only" />
        </label>
        <label class="block">
          <span class="text-xs text-zinc-500">Filter</span>
          <input
            class="w-full mt-0.5 px-2 py-1 rounded bg-zinc-100 dark:bg-zinc-800 mono border border-transparent focus:border-sky-500 outline-none"
            bind:value={editing.filter} />
        </label>
        <div>
          <span class="text-xs text-zinc-500">Save to</span>
          <div class="mt-1 flex gap-2">
            <button
              class="flex-1 px-2 py-1.5 rounded text-xs"
              class:bg-sky-600={editing.dest === "user"}
              class:text-white={editing.dest === "user"}
              class:bg-zinc-200={editing.dest !== "user"}
              class:dark:bg-zinc-800={editing.dest !== "user"}
              onclick={() => editing && (editing.dest = "user")}>
              User config<br><span class="text-[10px] opacity-70 mono">~/.zz/loggi/config.toml</span>
            </button>
            <button
              class="flex-1 px-2 py-1.5 rounded text-xs"
              class:bg-sky-600={editing.dest === "repo"}
              class:text-white={editing.dest === "repo"}
              class:bg-zinc-200={editing.dest !== "repo"}
              class:dark:bg-zinc-800={editing.dest !== "repo"}
              onclick={() => editing && (editing.dest = "repo")}>
              Repo config<br><span class="text-[10px] opacity-70 mono">REPO/.loggi/config.toml</span>
            </button>
          </div>
        </div>
        {#if error}
          <div class="text-red-500 text-xs">⚠ {error}</div>
        {/if}
      </div>
      <footer class="px-4 py-2.5 border-t border-zinc-200 dark:border-zinc-800 flex justify-end gap-2">
        <button
          class="px-3 py-1.5 rounded bg-zinc-200 dark:bg-zinc-800 text-xs"
          onclick={cancelEdit}
          disabled={saving}>Cancel</button>
        <button
          class="px-3 py-1.5 rounded bg-sky-600 text-white text-xs hover:bg-sky-700 disabled:opacity-50"
          onclick={save}
          disabled={saving}>{saving ? "Saving…" : "Save"}</button>
      </footer>
    {/if}
  </div>
</div>
