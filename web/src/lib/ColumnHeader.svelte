<script lang="ts">
  import type { Column } from "./columns";
  import { BUILTINS } from "./columns";

  let { columns, showTimestamps } = $props<{
    columns: Column[];
    showTimestamps: boolean;
  }>();

  function labelFor(c: Column): string {
    if (c.kind === "builtin" && BUILTINS[c.id]) return BUILTINS[c.id].label;
    return c.label || (c.kind === "field" ? c.id.replace(/^@/, "") : c.id);
  }
</script>

<div class="pl-4 pr-3 flex gap-3 py-1 text-[10px] uppercase tracking-wide text-zinc-500 dark:text-zinc-400 select-none">
  {#each columns as c (c.id)}
    {#if c.visible}
      {#if c.id === "ts"}
        {#if showTimestamps}
          <span class="shrink-0 truncate" style="width:{c.width}px">{labelFor(c)}</span>
        {/if}
      {:else if c.id === "msg"}
        <span class="truncate" class:flex-1={c.width === 0} class:shrink-0={c.width > 0}
              style={c.width ? `width:${c.width}px;flex:none` : ""}>{labelFor(c)}</span>
      {:else}
        <span class="shrink-0 truncate" style={c.width ? `width:${c.width}px` : ""}>{labelFor(c)}</span>
      {/if}
    {/if}
  {/each}
</div>
