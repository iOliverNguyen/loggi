<script lang="ts">
  import { onMount } from "svelte";
  import * as memory from "./source-memory";
  import DockerHoverCard from "./DockerHoverCard.svelte";

  let { onAdd, onClose } = $props<{
    onAdd: (kind: "file" | "docker", name: string, args: Record<string, unknown>) => void;
    onClose: () => void;
  }>();

  let hoverId = $state<string | null>(null);
  let containerAnchors = $state<Record<string, HTMLDivElement>>({});

  // Window-level keyboard handler so ←/→ work even when no element inside
  // the picker has focus (the per-element onkeydown only fires for events
  // bubbling from a focused descendant). Skipped while the user types in
  // the search box — we don't want to steal their cursor keys.
  function onWinKey(e: KeyboardEvent) {
    const t = e.target as HTMLElement | null;
    if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA")) return;
    if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
      e.preventDefault();
      tab = tab === "docker" ? "file" : "docker";
    }
  }

  type Kind = "file" | "docker";
  type Container = {
    id: string;
    names: string[];
    image: string;
    state: string;
    status: string;
    created: number;
  };

  let tab = $state<Kind>("docker");
  let dockerList = $state<Container[]>([]);
  // Declared after dockerList so svelte-check sees the dependency in
  // declaration order; functionally identical to a top-of-script $derived.
  let hoveredContainer = $derived(
    hoverId ? dockerList.find((c) => c.id === hoverId) ?? null : null,
  );
  let dockerErr = $state("");
  let dockerLoading = $state(false);
  let dockerSearch = $state("");

  let savedFiles = $state<string[]>(memory.saved("file"));
  let recentFiles = $state<string[]>(memory.recent("file"));
  let savedContainers = $state<string[]>(memory.saved("docker"));
  let recentContainers = $state<string[]>(memory.recent("docker"));

  let filePath = $state("");

  async function refreshDocker() {
    dockerLoading = true;
    dockerErr = "";
    try {
      const r = await fetch("/api/docker/containers");
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      const j = await r.json();
      dockerList = (j.containers ?? []) as Container[];
      if (j.error) dockerErr = j.error;
    } catch (e: any) {
      dockerErr = e?.message ?? "failed to list containers";
      dockerList = [];
    } finally {
      dockerLoading = false;
    }
  }

  onMount(() => {
    refreshDocker();
  });

  function addContainer(name: string) {
    onAdd("docker", name, {});
    recentContainers = memory.pushRecent("docker", name);
    onClose();
  }
  function addFile(path: string) {
    const p = path.trim();
    if (!p) return;
    onAdd("file", p, { path: p });
    recentFiles = memory.pushRecent("file", p);
    filePath = "";
    onClose();
  }
  function togglePin(kind: Kind, name: string) {
    if (kind === "docker") savedContainers = memory.toggleSaved(kind, name);
    else savedFiles = memory.toggleSaved(kind, name);
  }

  let filteredContainers = $derived(
    dockerSearch.trim() === ""
      ? dockerList
      : dockerList.filter((c) => {
          const q = dockerSearch.toLowerCase();
          return (
            c.names.some((n) => n.toLowerCase().includes(q)) ||
            c.image.toLowerCase().includes(q)
          );
        }),
  );

  // Set of currently-running container names for "saved but not running" hints.
  let runningSet = $derived(new Set(dockerList.flatMap((c) => c.names)));
</script>

<svelte:window onkeydown={onWinKey} />

<div
  class="rounded bg-zinc-100 dark:bg-zinc-900 p-2 mb-3 text-xs"
  role="tabpanel"
  onkeydown={(e) => { if (e.key === "Escape") onClose(); }}>
  <!-- tabs -->
  <div class="flex gap-1 mb-2 border-b border-zinc-200 dark:border-zinc-800 -mx-2 px-2" role="tablist">
    <button
      role="tab"
      aria-selected={tab === "docker"}
      class="px-3 py-1 -mb-px border-b-2 transition-colors"
      class:border-sky-500={tab === "docker"}
      class:text-sky-600={tab === "docker"}
      class:dark:text-sky-400={tab === "docker"}
      class:border-transparent={tab !== "docker"}
      class:text-zinc-500={tab !== "docker"}
      onclick={() => (tab = "docker")}>Docker</button>
    <button
      role="tab"
      aria-selected={tab === "file"}
      class="px-3 py-1 -mb-px border-b-2 transition-colors"
      class:border-sky-500={tab === "file"}
      class:text-sky-600={tab === "file"}
      class:dark:text-sky-400={tab === "file"}
      class:border-transparent={tab !== "file"}
      class:text-zinc-500={tab !== "file"}
      onclick={() => (tab = "file")}>File</button>
    <span class="flex-1"></span>
    <span class="self-center text-[10px] text-zinc-400 mono mr-1">←→ switch</span>
    <button
      class="text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 px-2"
      title="close (Esc)"
      onclick={onClose}>×</button>
  </div>

  {#if tab === "docker"}
    <!-- docker tab -->
    <div class="space-y-2">
      <div class="flex gap-1">
        <input
          class="flex-1 px-2 py-1 rounded bg-white dark:bg-zinc-800 mono"
          placeholder="filter containers…"
          bind:value={dockerSearch} />
        <button
          class="px-2 py-1 rounded bg-zinc-200 dark:bg-zinc-700 hover:bg-zinc-300 dark:hover:bg-zinc-600"
          title="refresh"
          onclick={refreshDocker}
          disabled={dockerLoading}>↻</button>
      </div>

      {#if dockerErr}
        <div class="text-red-500 text-[11px]">⚠ {dockerErr}</div>
      {/if}

      <!-- running -->
      <div>
        <div class="text-[10px] uppercase tracking-wider text-zinc-500 mb-1">
          Running {dockerLoading ? "…" : `(${filteredContainers.length})`}
        </div>
        <div class="max-h-40 overflow-y-auto space-y-0.5">
          {#each filteredContainers as c}
            {@const name = (c.names[0] ?? c.id.slice(0, 12)).replace(/^\//, "")}
            <div class="group relative flex items-center gap-1 px-1 py-0.5 rounded hover:bg-zinc-200 dark:hover:bg-zinc-800 cursor-pointer"
                 role="button"
                 tabindex="0"
                 bind:this={containerAnchors[c.id]}
                 onclick={() => addContainer(name)}
                 onkeydown={(e) => e.key === "Enter" && addContainer(name)}
                 onmouseenter={() => (hoverId = c.id)}
                 onmouseleave={() => { if (hoverId === c.id) hoverId = null; }}
                 onfocus={() => (hoverId = c.id)}
                 onblur={() => { if (hoverId === c.id) hoverId = null; }}>
              <span class="mono truncate flex-1" title={name}>{name}</span>
              <button
                class="opacity-0 group-hover:opacity-100 text-[10px] px-1 text-zinc-500 hover:text-amber-500"
                title={savedContainers.includes(name) ? "unpin" : "pin"}
                onclick={(e) => {
                  e.stopPropagation();
                  togglePin("docker", name);
                }}>{savedContainers.includes(name) ? "★" : "☆"}</button>
            </div>
          {/each}
          {#if hoverId && hoveredContainer}
            <DockerHoverCard container={hoveredContainer} anchor={containerAnchors[hoverId] ?? null} />
          {/if}
          {#if !dockerLoading && filteredContainers.length === 0}
            <div class="text-zinc-500 text-[11px] py-2 text-center">no running containers</div>
          {/if}
        </div>
      </div>

      {#if savedContainers.length > 0}
        <div>
          <div class="text-[10px] uppercase tracking-wider text-zinc-500 mb-1">Saved</div>
          <div class="space-y-0.5">
            {#each savedContainers as name}
              {@const running = runningSet.has(name)}
              <div class="group flex items-center gap-1 px-1 py-0.5 rounded hover:bg-zinc-200 dark:hover:bg-zinc-800 cursor-pointer"
                   role="button"
                   tabindex="0"
                   onclick={() => addContainer(name)}
                   onkeydown={(e) => e.key === "Enter" && addContainer(name)}>
                <span class="mono truncate flex-1" class:text-zinc-400={!running}>{name}</span>
                {#if !running}
                  <span class="text-[10px] text-amber-500">not running</span>
                {/if}
                <button
                  class="opacity-0 group-hover:opacity-100 text-[10px] px-1 text-amber-500"
                  title="unpin"
                  onclick={(e) => {
                    e.stopPropagation();
                    togglePin("docker", name);
                  }}>★</button>
              </div>
            {/each}
          </div>
        </div>
      {/if}

      {#if recentContainers.length > 0}
        <div>
          <div class="text-[10px] uppercase tracking-wider text-zinc-500 mb-1">Recent</div>
          <div class="flex flex-wrap gap-1">
            {#each recentContainers as name}
              <button
                class="px-1.5 py-0.5 rounded bg-white dark:bg-zinc-800 mono text-[11px] hover:bg-zinc-200 dark:hover:bg-zinc-700"
                onclick={() => addContainer(name)}>{name}</button>
            {/each}
          </div>
        </div>
      {/if}
    </div>
  {:else}
    <!-- file tab -->
    <div class="space-y-2">
      <div class="flex gap-1">
        <input
          class="flex-1 px-2 py-1 rounded bg-white dark:bg-zinc-800 mono"
          placeholder="/path/to/log"
          bind:value={filePath}
          onkeydown={(e) => e.key === "Enter" && addFile(filePath)} />
        <button
          class="px-3 py-1 rounded bg-sky-600 text-white hover:bg-sky-700"
          onclick={() => addFile(filePath)}>Add</button>
      </div>

      {#if savedFiles.length > 0}
        <div>
          <div class="text-[10px] uppercase tracking-wider text-zinc-500 mb-1">Saved</div>
          <div class="space-y-0.5">
            {#each savedFiles as path}
              <div class="group flex items-center gap-1 px-1 py-0.5 rounded hover:bg-zinc-200 dark:hover:bg-zinc-800 cursor-pointer"
                   role="button"
                   tabindex="0"
                   onclick={() => addFile(path)}
                   onkeydown={(e) => e.key === "Enter" && addFile(path)}>
                <span class="mono truncate flex-1" title={path}>{path}</span>
                <button
                  class="opacity-0 group-hover:opacity-100 text-[10px] px-1 text-amber-500"
                  title="unpin"
                  onclick={(e) => {
                    e.stopPropagation();
                    togglePin("file", path);
                  }}>★</button>
              </div>
            {/each}
          </div>
        </div>
      {/if}

      {#if recentFiles.length > 0}
        <div>
          <div class="text-[10px] uppercase tracking-wider text-zinc-500 mb-1">Recent</div>
          <div class="space-y-0.5">
            {#each recentFiles as path}
              <div class="group flex items-center gap-1 px-1 py-0.5 rounded hover:bg-zinc-200 dark:hover:bg-zinc-800 cursor-pointer"
                   role="button"
                   tabindex="0"
                   onclick={() => addFile(path)}
                   onkeydown={(e) => e.key === "Enter" && addFile(path)}>
                <span class="mono truncate flex-1 text-[11px]" title={path}>{path}</span>
                <button
                  class="opacity-0 group-hover:opacity-100 text-[10px] px-1 text-zinc-500 hover:text-amber-500"
                  title={memory.isSaved("file", path) ? "unpin" : "pin"}
                  onclick={(e) => {
                    e.stopPropagation();
                    togglePin("file", path);
                  }}>{savedFiles.includes(path) ? "★" : "☆"}</button>
              </div>
            {/each}
          </div>
        </div>
      {/if}
    </div>
  {/if}
</div>
