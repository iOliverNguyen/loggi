<script lang="ts">
  import { untrack } from "svelte";
  import Self from "./JsonTree.svelte";
  import Icon from "./Icon.svelte";

  let { value, path = [], onAddFilter, onReplaceFilter, isPathFiltered, depth = 0 } = $props<{
    value: unknown;
    path?: string[];
    onAddFilter?: (p: string[], v: unknown, negate: boolean, op?: "eq" | "exists") => void;
    onReplaceFilter?: (p: string[], v: unknown, negate: boolean, op?: "eq" | "exists") => void;
    isPathFiltered?: (p: string[]) => boolean;
    depth?: number;
  }>();

  let collapsed = $state(untrack(() => depth >= 3));
  let showFull = $state(false);
  const TRUNC = 200;

  function isObj(v: unknown): v is Record<string, unknown> {
    return v !== null && typeof v === "object" && !Array.isArray(v);
  }
  function isArr(v: unknown): v is unknown[] {
    return Array.isArray(v);
  }
  function isContainer(v: unknown): v is Record<string, unknown> | unknown[] {
    return v !== null && typeof v === "object";
  }

  function fieldRef(p: string[]): string {
    if (p.length === 0) return "";
    if (p.length === 1) return p[0];
    return "@" + p.join(".");
  }
  function valueLiteral(v: unknown): string {
    if (typeof v === "string") {
      return /[\s:()\[\]"]/.test(v) ? `"${v.replace(/"/g, '\\"')}"` : v;
    }
    return String(v);
  }
  function copy(s: string) {
    navigator.clipboard.writeText(s).catch(() => {});
  }

  function entries(v: Record<string, unknown> | unknown[]): [string, unknown][] {
    if (isArr(v)) return v.map((x, i) => [String(i), x] as [string, unknown]);
    return Object.entries(v);
  }

  function containerSize(v: Record<string, unknown> | unknown[]): string {
    return isArr(v) ? `[${v.length}]` : `{${Object.keys(v).length}}`;
  }
</script>

{#if path.length === 0}
  <!-- root: standalone object/array (called from DetailPanel) -->
  {#if isContainer(value)}
    <div class="font-mono text-[12px]">
      <button
        type="button"
        class="text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200 select-none"
        onclick={() => (collapsed = !collapsed)}
        title={collapsed ? "expand" : "collapse"}>
        {collapsed ? "▶" : "▼"}
        <span class="text-zinc-400">{containerSize(value)}</span>
      </button>
      {#if !collapsed}
        <ul class="pl-[2ch]">
          {#each entries(value) as [k, v]}
            <Self value={v} path={[k]} {onAddFilter} {onReplaceFilter} {isPathFiltered} depth={depth + 1} />
          {/each}
        </ul>
      {/if}
    </div>
  {/if}
{:else}
  {@const k = path[path.length - 1]}
  {@const isLeaf = !isContainer(value)}
  {@const filtered = isPathFiltered?.(path) ?? false}
  <li
    class="group leading-5 rounded -mx-1 px-1 transition-colors
           hover:bg-sky-50 dark:hover:bg-sky-950/40 hover:ring-1 hover:ring-sky-200 dark:hover:ring-sky-900"
    class:bg-sky-100={filtered}
    class:dark:bg-sky-900={filtered}
    class:ring-1={filtered}
    class:ring-sky-300={filtered}
    class:dark:ring-sky-800={filtered}>
    <div class="flex items-start gap-1">
      <span class="text-violet-700 dark:text-violet-300 shrink-0">{k}</span>
      <span class="text-zinc-400 shrink-0">:</span>
      <div class="flex-1 min-w-0">
        {#if isContainer(value)}
          <button
            type="button"
            class="text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200 select-none font-mono text-[12px]"
            onclick={() => (collapsed = !collapsed)}
            title={collapsed ? "expand" : "collapse"}>
            {collapsed ? "▶" : "▼"}
            <span class="text-zinc-400">{containerSize(value)}</span>
          </button>
        {:else if value === null}
          <span class="text-zinc-400 italic font-mono text-[12px]">null</span>
        {:else if typeof value === "string"}
          {@const truncated = value.length > TRUNC && !showFull}
          <span class="text-emerald-700 dark:text-emerald-400 break-all whitespace-pre-wrap font-mono text-[12px]">
            {truncated ? value.slice(0, TRUNC) + "…" : value}
          </span>
          {#if value.length > TRUNC}
            <button
              class="text-[10px] ml-1 text-sky-600 hover:underline"
              onclick={() => (showFull = !showFull)}>
              {showFull ? "less" : "more"}
            </button>
          {/if}
        {:else if typeof value === "number"}
          <span class="text-amber-700 dark:text-amber-400 font-mono text-[12px]">{value}</span>
        {:else if typeof value === "boolean"}
          <span class="text-fuchsia-700 dark:text-fuchsia-400 font-mono text-[12px]">{value}</span>
        {:else}
          <span class="text-zinc-500 font-mono text-[12px]">{String(value)}</span>
        {/if}
      </div>
      {#if onAddFilter}
        <span class="opacity-0 group-hover:opacity-100 transition-opacity flex gap-0.5 shrink-0">
          {#if isLeaf}
            <button
              class="p-0.5 rounded text-emerald-700 dark:text-emerald-400 hover:bg-emerald-600/15"
              title={`filter ${fieldRef(path)}:${valueLiteral(value)}`}
              onclick={() => onAddFilter(path, value, false)}
              aria-label="add filter">
              <Icon name="plus" size={12} />
            </button>
            {#if onReplaceFilter}
              <button
                class="p-0.5 rounded text-sky-700 dark:text-sky-400 hover:bg-sky-600/15"
                title={`filter only ${fieldRef(path)}:${valueLiteral(value)}`}
                onclick={() => onReplaceFilter(path, value, false)}
                aria-label="replace all filters with this">
                <Icon name="crosshair" size={12} />
              </button>
            {/if}
            <button
              class="p-0.5 rounded text-rose-700 dark:text-rose-400 hover:bg-rose-600/15"
              title={`filter -${fieldRef(path)}:${valueLiteral(value)}`}
              onclick={() => onAddFilter(path, value, true)}
              aria-label="exclude filter">
              <Icon name="minus" size={12} />
            </button>
            <button
              class="p-0.5 rounded text-zinc-500 hover:bg-zinc-200 dark:hover:bg-zinc-800 hover:text-zinc-800 dark:hover:text-zinc-200"
              title="copy value"
              onclick={() => copy(typeof value === "string" ? value : String(value))}
              aria-label="copy value">
              <Icon name="copy" size={12} />
            </button>
          {:else}
            <button
              class="p-0.5 rounded text-emerald-700 dark:text-emerald-400 hover:bg-emerald-600/15"
              title={`filter ${fieldRef(path)}:* (field is set)`}
              onclick={() => onAddFilter(path, undefined, false, "exists")}
              aria-label="filter field is set">
              <Icon name="plus" size={12} />
            </button>
            {#if onReplaceFilter}
              <button
                class="p-0.5 rounded text-sky-700 dark:text-sky-400 hover:bg-sky-600/15"
                title={`filter only ${fieldRef(path)}:* (field is set)`}
                onclick={() => onReplaceFilter(path, undefined, false, "exists")}
                aria-label="replace all filters with this">
                <Icon name="crosshair" size={12} />
              </button>
            {/if}
            <button
              class="p-0.5 rounded text-rose-700 dark:text-rose-400 hover:bg-rose-600/15"
              title={`filter -${fieldRef(path)}:* (field is not set)`}
              onclick={() => onAddFilter(path, undefined, true, "exists")}
              aria-label="filter field is not set">
              <Icon name="minus" size={12} />
            </button>
            <button
              class="p-0.5 rounded text-zinc-500 hover:bg-zinc-200 dark:hover:bg-zinc-800 hover:text-zinc-800 dark:hover:text-zinc-200"
              title="copy path"
              onclick={() => copy(fieldRef(path))}
              aria-label="copy path">
              <Icon name="copy" size={12} />
            </button>
          {/if}
        </span>
      {/if}
    </div>
    {#if isContainer(value) && !collapsed}
      <ul class="pl-[2ch]">
        {#each entries(value) as [ck, cv]}
          <Self value={cv} path={[...path, ck]} {onAddFilter} {onReplaceFilter} {isPathFiltered} depth={depth + 1} />
        {/each}
      </ul>
    {/if}
  </li>
{/if}
