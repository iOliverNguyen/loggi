<script lang="ts">
  import Icon from "./Icon.svelte";

  let { fieldValues, pendingFilter, onAddClause, onRemoveClause, onReplace, isClauseActive, pinnedOnly = false } = $props<{
    fieldValues: Map<string, Map<string, number>>;
    pendingFilter: string;
    onAddClause: (clause: string) => void;
    onRemoveClause: (clause: string) => void;
    onReplace: (clause: string) => void;
    isClauseActive: (clause: string) => boolean;
    pinnedOnly?: boolean;
  }>();

  const TOP_VALUES = 8;
  const SEARCH_THRESHOLD = 10;

  // Persisted preferences.
  function loadSet(key: string): Set<string> {
    try {
      const arr = JSON.parse(localStorage.getItem(key) ?? "[]");
      return new Set(Array.isArray(arr) ? arr : []);
    } catch {
      return new Set();
    }
  }
  function saveSet(key: string, set: Set<string>) {
    try { localStorage.setItem(key, JSON.stringify([...set])); } catch {}
  }
  function loadMap(key: string): Map<string, Set<string>> {
    try {
      const obj = JSON.parse(localStorage.getItem(key) ?? "{}");
      const m = new Map<string, Set<string>>();
      if (obj && typeof obj === "object") {
        for (const [k, v] of Object.entries(obj)) {
          if (Array.isArray(v)) m.set(k, new Set(v as string[]));
        }
      }
      return m;
    } catch {
      return new Map();
    }
  }
  function saveMap(key: string, m: Map<string, Set<string>>) {
    try {
      const obj: Record<string, string[]> = {};
      for (const [k, v] of m) obj[k] = [...v];
      localStorage.setItem(key, JSON.stringify(obj));
    } catch {}
  }

  let pinnedKeys = $state(loadSet("loggi.facets.pinnedKeys"));
  let pinnedValues = $state(loadMap("loggi.facets.pinnedValues"));
  let openKeys = $state(loadSet("loggi.facets.openKeys"));
  let expandedKeys = $state(new Set<string>()); // "show more" expanded per key
  let valueSearch = $state(new Map<string, string>());

  $effect(() => saveSet("loggi.facets.pinnedKeys", pinnedKeys));
  $effect(() => saveMap("loggi.facets.pinnedValues", pinnedValues));
  $effect(() => saveSet("loggi.facets.openKeys", openKeys));

  function fieldRef(key: string): string {
    return key.includes(".") ? "@" + key : key;
  }

  function valueLiteral(v: string): string {
    return /[\s:()\[\]"]/.test(v) ? `"${v.replace(/"/g, '\\"')}"` : v;
  }

  function clauseFor(key: string, value: string): string {
    return `${fieldRef(key)}:${valueLiteral(value)}`;
  }

  // Score: average count per distinct value. High score = few values dominate
  // (good for facets). Keys with a single value get score 0 (no info).
  function keyScore(values: Map<string, number>): number {
    if (values.size <= 1) return 0;
    let total = 0;
    for (const c of values.values()) total += c;
    return total / values.size;
  }

  let sortedKeys = $derived.by(() => {
    const arr: { key: string; values: Map<string, number>; score: number; pinned: boolean }[] = [];
    for (const [key, values] of fieldValues) {
      const pinned = pinnedKeys.has(key);
      if (pinnedOnly && !pinned) continue;
      arr.push({ key, values, score: keyScore(values), pinned });
    }
    arr.sort((a, b) => {
      if (a.pinned !== b.pinned) return a.pinned ? -1 : 1;
      return b.score - a.score;
    });
    return arr;
  });

  function togglePinKey(key: string) {
    if (pinnedKeys.has(key)) pinnedKeys.delete(key);
    else pinnedKeys.add(key);
    pinnedKeys = new Set(pinnedKeys);
  }

  function togglePinValue(key: string, value: string) {
    let set = pinnedValues.get(key);
    if (!set) {
      set = new Set();
      pinnedValues.set(key, set);
    }
    if (set.has(value)) set.delete(value);
    else set.add(value);
    if (set.size === 0) pinnedValues.delete(key);
    pinnedValues = new Map(pinnedValues);
  }

  function toggleOpen(key: string) {
    if (openKeys.has(key)) openKeys.delete(key);
    else openKeys.add(key);
    openKeys = new Set(openKeys);
  }

  function toggleExpand(key: string) {
    if (expandedKeys.has(key)) expandedKeys.delete(key);
    else expandedKeys.add(key);
    expandedKeys = new Set(expandedKeys);
  }

  function valuesFor(key: string, values: Map<string, number>): { value: string; count: number; pinned: boolean }[] {
    const pinnedSet = pinnedValues.get(key) ?? new Set<string>();
    const search = (valueSearch.get(key) ?? "").toLowerCase();
    const out: { value: string; count: number; pinned: boolean }[] = [];
    for (const [value, count] of values) {
      if (search && !value.toLowerCase().includes(search)) continue;
      out.push({ value, count, pinned: pinnedSet.has(value) });
    }
    out.sort((a, b) => {
      if (a.pinned !== b.pinned) return a.pinned ? -1 : 1;
      return b.count - a.count;
    });
    return out;
  }

  function onValueClick(key: string, value: string) {
    const clause = clauseFor(key, value);
    if (isClauseActive(clause)) onRemoveClause(clause);
    else onAddClause(clause);
  }
</script>

{#if sortedKeys.length === 0}
  {#if !pinnedOnly}<p class="text-[11px] text-zinc-500">No fields seen yet.</p>{/if}
{:else}
  <ul class="space-y-0.5">
    {#each sortedKeys as { key, values, pinned } (key)}
      {@const isOpen = openKeys.has(key)}
      {@const isExpanded = expandedKeys.has(key)}
      {@const list = isOpen ? valuesFor(key, values) : []}
      {@const visible = isExpanded ? list : list.slice(0, TOP_VALUES)}
      {@const hidden = list.length - visible.length}
      <li class="group">
        <div class="flex items-center gap-1 px-1 -mx-1 rounded hover:bg-zinc-100 dark:hover:bg-zinc-800">
          <button
            type="button"
            class="flex items-center gap-1 min-w-0 flex-1 text-left"
            onclick={() => toggleOpen(key)}
            aria-expanded={isOpen}>
            <Icon name={isOpen ? "chevron-down" : "chevron-right"} size={10} />
            <span class="mono text-[11px] truncate text-zinc-700 dark:text-zinc-200" title={key}>{key}</span>
            <span class="text-[10px] text-zinc-400">{values.size}</span>
          </button>
          <button
            type="button"
            class="p-0.5 rounded transition-opacity"
            class:opacity-0={!pinned}
            class:group-hover:opacity-100={!pinned}
            class:text-amber-500={pinned}
            class:text-zinc-400={!pinned}
            class:hover:text-amber-600={!pinned}
            title={pinned ? "Unpin facet" : "Pin facet to top"}
            onclick={() => togglePinKey(key)}
            aria-label={pinned ? "unpin facet" : "pin facet"}
            aria-pressed={pinned}>
            <Icon name={pinned ? "star-filled" : "star"} size={10} />
          </button>
        </div>
        {#if isOpen}
          {#if values.size > SEARCH_THRESHOLD}
            <input
              type="text"
              class="w-full mt-0.5 mb-0.5 px-1.5 py-0.5 text-[11px] rounded bg-zinc-100 dark:bg-zinc-800 border border-transparent focus:border-sky-500 focus:outline-none"
              placeholder={`Search ${values.size} values…`}
              value={valueSearch.get(key) ?? ""}
              oninput={(e) => {
                const m = new Map(valueSearch);
                m.set(key, (e.currentTarget as HTMLInputElement).value);
                valueSearch = m;
              }} />
          {/if}
          <ul class="ml-3 space-y-px">
            {#each visible as v (v.value)}
              {@const clause = clauseFor(key, v.value)}
              {@const active = isClauseActive(clause)}
              <li class="group/v">
                <div
                  class="flex items-center gap-1 px-1.5 py-0.5 rounded cursor-pointer"
                  class:bg-sky-100={active}
                  class:dark:bg-sky-900={active}
                  class:hover:bg-zinc-100={!active}
                  class:dark:hover:bg-zinc-800={!active}
                  onclick={() => onValueClick(key, v.value)}
                  role="button"
                  tabindex="0"
                  onkeydown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onValueClick(key, v.value); } }}
                  title={active ? `remove ${clause}` : `add ${clause}`}>
                  <span class="mono text-[11px] truncate flex-1 min-w-0" class:text-sky-700={active} class:dark:text-sky-300={active}>{v.value}</span>
                  <span class="text-[10px] text-zinc-400 mono shrink-0">{v.count}</span>
                  <button
                    type="button"
                    class={`p-0.5 rounded transition-opacity shrink-0 ${v.pinned ? "text-amber-500 hover:text-amber-600" : "opacity-0 group-hover/v:opacity-100 text-zinc-400 hover:text-amber-600"}`}
                    title={v.pinned ? "Unpin value" : "Pin value to top"}
                    onclick={(e) => { e.stopPropagation(); togglePinValue(key, v.value); }}
                    aria-label={v.pinned ? "unpin value" : "pin value"}
                    aria-pressed={v.pinned}>
                    <Icon name={v.pinned ? "star-filled" : "star"} size={9} />
                  </button>
                </div>
              </li>
            {/each}
            {#if hidden > 0}
              <li>
                <button
                  type="button"
                  class="text-[10px] text-sky-600 hover:underline ml-1.5"
                  onclick={() => toggleExpand(key)}>+ {hidden} more</button>
              </li>
            {:else if isExpanded && list.length > TOP_VALUES}
              <li>
                <button
                  type="button"
                  class="text-[10px] text-sky-600 hover:underline ml-1.5"
                  onclick={() => toggleExpand(key)}>show less</button>
              </li>
            {/if}
          </ul>
        {/if}
      </li>
    {/each}
  </ul>
{/if}
