<script lang="ts">
  import { tick } from "svelte";
  import Icon from "./Icon.svelte";

  type Theme = "auto" | "light" | "dark";
  type Density = "compact" | "cozy" | "comfortable";

  let {
    theme,
    density,
    showQuickBar,
    showTimestamps,
    showTimeline,
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

  function confirmClearLocal() {
    if (window.confirm("Reset all local settings (filter history, quick chips, density, theme, etc.)? This won't touch server-side profiles.")) {
      onClearLocal();
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
    class="bg-white dark:bg-zinc-900 rounded-lg shadow-xl w-[520px] max-h-[85vh] flex flex-col text-sm outline-none"
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
                  onclick={() => onChangeTheme(t.v)}>{t.label}</button>
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
                  onclick={() => onChangeDensity(d.v)}>{d.label}</button>
              {/each}
            </div>
          </div>
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
        <h3 class="text-[10px] uppercase tracking-wider text-zinc-500 font-semibold mb-2">Data</h3>
        <div class="space-y-2">
          <button
            class="w-full text-left px-3 py-2 rounded bg-zinc-100 dark:bg-zinc-800 hover:bg-zinc-200 dark:hover:bg-zinc-700 text-xs inline-flex items-center justify-between"
            onclick={onOpenProfiles}>
            <span class="inline-flex items-center gap-2"><Icon name="edit" size={12} /> Manage profiles…</span>
            <Icon name="chevron-right" size={12} />
          </button>
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
