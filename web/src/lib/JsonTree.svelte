<script lang="ts">
  import { untrack } from "svelte";
  import Self from "./JsonTree.svelte";

  let { value, path = [], onAddFilter, depth = 0 } = $props<{
    value: unknown;
    path?: string[];
    onAddFilter?: (p: string[], v: unknown, negate: boolean) => void;
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
</script>

{#if isObj(value) || isArr(value)}
  <div class="font-mono text-[12px]">
    <button
      type="button"
      class="text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200 select-none"
      onclick={() => (collapsed = !collapsed)}
      title={collapsed ? "expand" : "collapse"}>
      {collapsed ? "▶" : "▼"}
      <span class="text-zinc-400">
        {isArr(value) ? `[${(value as unknown[]).length}]` : `{${Object.keys(value as object).length}}`}
      </span>
    </button>
    {#if !collapsed}
      <ul class="ml-4 border-l border-zinc-200 dark:border-zinc-800 pl-2">
        {#each entries(value) as [k, v]}
          {@const childPath = [...path, k]}
          <li class="group flex items-start gap-1 leading-5 hover:bg-zinc-50 dark:hover:bg-zinc-900/50 rounded px-1 -mx-1">
            <span class="text-violet-700 dark:text-violet-300 shrink-0">{k}</span>
            <span class="text-zinc-400 shrink-0">:</span>
            <div class="flex-1 min-w-0">
              <Self value={v} path={childPath} {onAddFilter} depth={depth + 1} />
            </div>
            {#if onAddFilter && (typeof v === "string" || typeof v === "number" || typeof v === "boolean")}
              <span class="opacity-0 group-hover:opacity-100 transition-opacity flex gap-0.5 shrink-0 text-[10px]">
                <button
                  class="px-1 rounded bg-emerald-600/10 text-emerald-700 dark:text-emerald-400 hover:bg-emerald-600/20"
                  title={`filter ${fieldRef(childPath)}:${valueLiteral(v)}`}
                  onclick={() => onAddFilter(childPath, v, false)}>+</button>
                <button
                  class="px-1 rounded bg-rose-600/10 text-rose-700 dark:text-rose-400 hover:bg-rose-600/20"
                  title={`filter -${fieldRef(childPath)}:${valueLiteral(v)}`}
                  onclick={() => onAddFilter(childPath, v, true)}>−</button>
                <button
                  class="px-1 rounded bg-zinc-200 dark:bg-zinc-800 text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200"
                  title="copy path"
                  onclick={() => copy(fieldRef(childPath))}>p</button>
                <button
                  class="px-1 rounded bg-zinc-200 dark:bg-zinc-800 text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200"
                  title="copy value"
                  onclick={() => copy(typeof v === "string" ? v : String(v))}>v</button>
              </span>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </div>
{:else if value === null}
  <span class="text-zinc-400 italic">null</span>
{:else if typeof value === "string"}
  {@const truncated = value.length > TRUNC && !showFull}
  <span class="text-emerald-700 dark:text-emerald-400 break-all whitespace-pre-wrap">
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
  <span class="text-amber-700 dark:text-amber-400">{value}</span>
{:else if typeof value === "boolean"}
  <span class="text-fuchsia-700 dark:text-fuchsia-400">{value}</span>
{:else}
  <span class="text-zinc-500">{String(value)}</span>
{/if}
