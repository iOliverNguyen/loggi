<script lang="ts">
  import { tick, onMount } from "svelte";
  import Icon from "./Icon.svelte";
  import AddSourceTabs from "./AddSourceTabs.svelte";

  type Theme = "auto" | "light" | "dark";
  type Density = "compact" | "cozy" | "comfortable";
  type SourceRef = { kind: string; name: string; args?: Record<string, unknown> };

  let {
    theme,
    density,
    showQuickBar,
    showTimestamps,
    showTimeline,
    profileNames = [],
    onChangeTheme,
    onChangeDensity,
    onChangeShowQuickBar,
    onChangeShowTimestamps,
    onChangeShowTimeline,
    onClearHistory,
    onClearQuickChips,
    onClearLocal,
    onOpenProfiles,
    onClose,
  } = $props<{
    theme: Theme;
    density: Density;
    showQuickBar: boolean;
    showTimestamps: boolean;
    showTimeline: boolean;
    profileNames?: string[];
    onChangeTheme: (t: Theme) => void;
    onChangeDensity: (d: Density) => void;
    onChangeShowQuickBar: (v: boolean) => void;
    onChangeShowTimestamps: (v: boolean) => void;
    onChangeShowTimeline: (v: boolean) => void;
    onClearHistory: () => void;
    onClearQuickChips: () => void;
    onClearLocal: () => void;
    onOpenProfiles: () => void;
    onClose: () => void;
  }>();

  let dialogEl: HTMLDivElement | null = $state(null);
  $effect(() => { if (dialogEl) tick().then(() => dialogEl?.focus()); });

  // Server-side config (fetched on mount). Theme/density mirror the local
  // state above; the others are server-managed.
  let timestampFormat = $state("");
  let defaultProfile = $state("");
  let filePollMS = $state<number>(0);
  let dockerTail = $state<number>(0);
  let serverInfo = $state<{ idle_timeout: string; ring_buffer: number; http_bind: string } | null>(null);
  let autostart = $state<SourceRef[]>([]);
  let configError = $state("");
  let showAddPicker = $state(false);

  onMount(async () => {
    try {
      const r = await fetch("/api/config");
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      const j = await r.json();
      timestampFormat = j.timestamp_format ?? "";
      defaultProfile = j.default_profile ?? "";
      filePollMS = j.source_defaults?.file_poll_ms ?? 0;
      dockerTail = j.source_defaults?.docker_tail ?? 0;
      serverInfo = j.server ?? null;
      autostart = Array.isArray(j.autostart) ? j.autostart : [];
    } catch (e: any) {
      configError = e?.message ?? "failed to load config";
    }
  });

  // Write-through: update local state immediately, then POST. On failure,
  // surface the error but don't roll back — the user can re-edit.
  async function patch(body: Record<string, unknown>) {
    try {
      const r = await fetch("/api/config", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!r.ok) {
        const txt = await r.text();
        configError = `save failed: ${txt}`;
      } else {
        configError = "";
      }
    } catch (e: any) {
      configError = e?.message ?? "save failed";
    }
  }

  function selectTheme(t: Theme) {
    onChangeTheme(t);
    patch({ theme: t });
  }
  function selectDensity(d: Density) {
    onChangeDensity(d);
    patch({ density: d });
  }
  function onTimestampBlur() {
    patch({ timestamp_format: timestampFormat });
  }
  function onDefaultProfileChange(e: Event) {
    defaultProfile = (e.currentTarget as HTMLSelectElement).value;
    patch({ default_profile: defaultProfile });
  }
  function onFilePollBlur() {
    patch({ source_defaults: { file_poll_ms: filePollMS } });
  }
  function onDockerTailBlur() {
    patch({ source_defaults: { docker_tail: dockerTail } });
  }

  async function persistAutostart() {
    try {
      const r = await fetch("/api/config/autostart", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ autostart }),
      });
      if (!r.ok) configError = `autostart save failed: ${await r.text()}`;
    } catch (e: any) {
      configError = e?.message ?? "autostart save failed";
    }
  }

  // Best-effort: if a live source matches (kind, name), remove it too so
  // the user doesn't have to restart loggi just to retire an autostart entry.
  async function removeAutostart(i: number) {
    const ref = autostart[i];
    autostart = autostart.filter((_, idx) => idx !== i);
    await persistAutostart();
    try {
      const r = await fetch("/api/sources");
      if (!r.ok) return;
      const live = (await r.json()) as Array<{ id: number; kind: string; name: string }>;
      const match = live.find((s) => s.kind === ref.kind && s.name === ref.name);
      if (match) {
        await fetch(`/api/sources?id=${match.id}`, { method: "DELETE" });
      }
    } catch {
      /* surfacing this would be noisy; the autostart entry is already removed from disk */
    }
  }

  // Persist to autostart AND start the source live so the user sees data
  // immediately. Avoids a "saved but not running until restart" footgun.
  async function onAddAutostart(kind: "file" | "docker", name: string, args: Record<string, unknown>) {
    showAddPicker = false;
    if (autostart.some((r) => r.kind === kind && r.name === name)) return;
    autostart = [...autostart, { kind, name, args }];
    await persistAutostart();
    try {
      await fetch("/api/sources", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ kind, name, args }),
      });
    } catch (e: any) {
      configError = e?.message ?? "added to autostart, but failed to start now";
    }
  }

  function confirmClearLocal() {
    if (window.confirm("Reset all local settings (filter history, quick chips, density, theme, etc.)? This won't touch server-side profiles.")) {
      onClearLocal();
    }
  }

  const THEMES: { v: Theme; label: string }[] = [
    { v: "auto", label: "Auto" },
    { v: "light", label: "Light" },
    { v: "dark", label: "Dark" },
  ];
  const DENSITIES: { v: Density; label: string }[] = [
    { v: "compact", label: "Compact" },
    { v: "cozy", label: "Cozy" },
    { v: "comfortable", label: "Comfortable" },
  ];
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
        <Icon name="settings" size={14} /> Settings
      </h2>
      <button
        class="text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100"
        onclick={onClose}
        aria-label="close">
        <Icon name="x" size={16} />
      </button>
    </header>

    <div class="flex-1 overflow-y-auto px-4 py-3 space-y-5">
      {#if configError}
        <div class="text-[11px] text-red-500 px-2 py-1 rounded bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-900/40">
          ⚠ {configError}
        </div>
      {/if}

      <section>
        <h3 class="text-[10px] uppercase tracking-wider text-zinc-500 font-semibold mb-2">Display</h3>
        <div class="space-y-3">
          <div class="flex items-center justify-between gap-3">
            <span class="text-xs">Theme</span>
            <div class="flex gap-1">
              {#each THEMES as t}
                <button
                  class="px-2 py-1 rounded text-[11px]"
                  class:bg-sky-600={theme === t.v}
                  class:text-white={theme === t.v}
                  class:bg-zinc-100={theme !== t.v}
                  class:dark:bg-zinc-800={theme !== t.v}
                  onclick={() => selectTheme(t.v)}>{t.label}</button>
              {/each}
            </div>
          </div>
          <div class="flex items-center justify-between gap-3">
            <span class="text-xs">Density</span>
            <div class="flex gap-1">
              {#each DENSITIES as d}
                <button
                  class="px-2 py-1 rounded text-[11px]"
                  class:bg-sky-600={density === d.v}
                  class:text-white={density === d.v}
                  class:bg-zinc-100={density !== d.v}
                  class:dark:bg-zinc-800={density !== d.v}
                  onclick={() => selectDensity(d.v)}>{d.label}</button>
              {/each}
            </div>
          </div>
          <label class="flex items-center justify-between gap-3 cursor-pointer">
            <span class="text-xs">Timestamp format</span>
            <input
              class="px-2 py-1 rounded bg-zinc-100 dark:bg-zinc-800 mono text-[11px] w-44 border border-transparent focus:border-sky-500 outline-none"
              placeholder="15:04:05.000"
              bind:value={timestampFormat}
              onblur={onTimestampBlur} />
          </label>
          <label class="flex items-center justify-between gap-3 cursor-pointer">
            <span class="text-xs">Show timestamps</span>
            <input type="checkbox" checked={showTimestamps} onchange={(e) => onChangeShowTimestamps((e.currentTarget as HTMLInputElement).checked)} />
          </label>
          <label class="flex items-center justify-between gap-3 cursor-pointer">
            <span class="text-xs">Show Quick filter bar</span>
            <input type="checkbox" checked={showQuickBar} onchange={(e) => onChangeShowQuickBar((e.currentTarget as HTMLInputElement).checked)} />
          </label>
          <label class="flex items-center justify-between gap-3 cursor-pointer">
            <span class="text-xs">Show timeline strip</span>
            <input type="checkbox" checked={showTimeline} onchange={(e) => onChangeShowTimeline((e.currentTarget as HTMLInputElement).checked)} />
          </label>
        </div>
      </section>

      <section>
        <h3 class="text-[10px] uppercase tracking-wider text-zinc-500 font-semibold mb-2">Profiles</h3>
        <div class="space-y-2">
          {#if profileNames.length > 0}
            <label class="flex items-center justify-between gap-3">
              <span class="text-xs">Default profile on launch</span>
              <select
                class="px-2 py-1 rounded bg-zinc-100 dark:bg-zinc-800 text-[11px] mono"
                value={defaultProfile}
                onchange={onDefaultProfileChange}>
                <option value="">— none —</option>
                {#each profileNames as n}
                  <option value={n}>{n}</option>
                {/each}
              </select>
            </label>
          {/if}
          <button
            class="w-full text-left px-3 py-2 rounded bg-zinc-100 dark:bg-zinc-800 hover:bg-zinc-200 dark:hover:bg-zinc-700 text-xs inline-flex items-center justify-between"
            onclick={onOpenProfiles}>
            <span class="inline-flex items-center gap-2"><Icon name="edit" size={12} /> Manage profiles…</span>
            <Icon name="chevron-right" size={12} />
          </button>
        </div>
      </section>

      <section>
        <h3 class="text-[10px] uppercase tracking-wider text-zinc-500 font-semibold mb-2 flex items-center justify-between">
          <span>Sources — Autostart on launch</span>
          <button
            class="text-[10px] text-sky-600 hover:text-sky-700 normal-case"
            onclick={() => (showAddPicker = !showAddPicker)}>{showAddPicker ? "cancel" : "+ add"}</button>
        </h3>
        {#if showAddPicker}
          <AddSourceTabs onAdd={onAddAutostart} onClose={() => (showAddPicker = false)} />
        {/if}
        <div class="space-y-1">
          {#each autostart as ref, i}
            <div class="group flex items-center gap-2 px-2 py-1 rounded bg-zinc-50 dark:bg-zinc-800/70 text-[11px]">
              <span class="text-[9px] uppercase tracking-wider text-zinc-500 w-12">{ref.kind}</span>
              <span class="mono flex-1 truncate" title={ref.name}>{ref.name}</span>
              <button
                class="opacity-0 group-hover:opacity-100 text-red-500 hover:text-red-600 px-1"
                title="remove from autostart"
                onclick={() => removeAutostart(i)}>×</button>
            </div>
          {:else}
            <div class="text-[11px] text-zinc-500 italic">No sources will start automatically.</div>
          {/each}
        </div>
        <div class="grid grid-cols-2 gap-2 mt-3">
          <label class="flex items-center justify-between gap-2">
            <span class="text-xs">File poll (ms)</span>
            <input
              type="number"
              min="1"
              class="w-20 px-2 py-1 rounded bg-zinc-100 dark:bg-zinc-800 mono text-[11px] text-right"
              bind:value={filePollMS}
              onblur={onFilePollBlur} />
          </label>
          <label class="flex items-center justify-between gap-2">
            <span class="text-xs">Docker tail</span>
            <input
              type="number"
              min="0"
              class="w-20 px-2 py-1 rounded bg-zinc-100 dark:bg-zinc-800 mono text-[11px] text-right"
              bind:value={dockerTail}
              onblur={onDockerTailBlur} />
          </label>
        </div>
      </section>

      {#if serverInfo}
        <section>
          <h3 class="text-[10px] uppercase tracking-wider text-zinc-500 font-semibold mb-2">Server (read-only)</h3>
          <div class="text-[11px] mono text-zinc-600 dark:text-zinc-400 space-y-0.5 px-2 py-2 rounded bg-zinc-50 dark:bg-zinc-800/70">
            <div>idle_timeout = {serverInfo.idle_timeout}</div>
            <div>ring_buffer  = {serverInfo.ring_buffer}</div>
            <div>http_bind    = {serverInfo.http_bind}</div>
          </div>
          <div class="text-[10px] text-zinc-500 mt-1">Edit <code class="mono">~/.zz/loggi/config.toml</code> and restart loggi to change these.</div>
        </section>
      {/if}

      <section>
        <h3 class="text-[10px] uppercase tracking-wider text-zinc-500 font-semibold mb-2">Maintenance</h3>
        <div class="space-y-2">
          <button
            class="w-full text-left px-3 py-2 rounded bg-zinc-100 dark:bg-zinc-800 hover:bg-zinc-200 dark:hover:bg-zinc-700 text-xs inline-flex items-center justify-between"
            onclick={onClearHistory}>
            <span class="inline-flex items-center gap-2"><Icon name="trash" size={12} /> Clear filter history</span>
          </button>
          <button
            class="w-full text-left px-3 py-2 rounded bg-zinc-100 dark:bg-zinc-800 hover:bg-zinc-200 dark:hover:bg-zinc-700 text-xs inline-flex items-center justify-between"
            onclick={onClearQuickChips}>
            <span class="inline-flex items-center gap-2"><Icon name="trash" size={12} /> Reset quick filter chips to defaults</span>
          </button>
          <button
            class="w-full text-left px-3 py-2 rounded text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 text-xs inline-flex items-center justify-between border border-red-200 dark:border-red-900/40"
            onclick={confirmClearLocal}>
            <span class="inline-flex items-center gap-2"><Icon name="refresh" size={12} /> Reset all local settings</span>
          </button>
        </div>
      </section>
    </div>
  </div>
</div>
