<script lang="ts">
  import type { Entry } from "./types";
  import type { Column } from "./columns";
  import { readFieldPath } from "./columns";

  let {
    entry,
    columns,
    showTimestamps,
    levelClass,
    sourceName,
    fmtTs,
    onSourceClick,
    msgHTML,
  } = $props<{
    entry: Entry;
    columns: Column[];
    showTimestamps: boolean;
    levelClass: (l?: string) => string;
    sourceName: (id: number) => string;
    fmtTs: (ts: number) => string;
    onSourceClick: (e: MouseEvent, name: string) => void;
    // Returns either plain string (rendered as text) or { html: string }
    // (rendered with @html for ansi / highlight).
    msgHTML?: (e: Entry) => string | { html: string };
  }>();

  function fieldValue(c: Column): string {
    if (c.kind !== "field") return "";
    const path = c.id.startsWith("@") ? c.id.slice(1) : c.id;
    return readFieldPath(entry.fields, path);
  }
</script>

{#each columns as c (c.id)}
  {#if c.visible}
    {#if c.id === "ts"}
      {#if showTimestamps}
        <span class="text-zinc-500 shrink-0 truncate" style={c.width ? `width:${c.width}px` : ""}>
          {fmtTs(entry.ts)}
        </span>
      {/if}
    {:else if c.id === "level"}
      <span class={`shrink-0 truncate ${levelClass(entry.level)}`} style={c.width ? `width:${c.width}px` : ""}>
        {(entry.level ?? "").toUpperCase()}
      </span>
    {:else if c.id === "source"}
      <button
        type="button"
        class="shrink-0 truncate text-zinc-500 text-[11px] text-left hover:text-sky-600 dark:hover:text-sky-400"
        style={c.width ? `width:${c.width}px` : ""}
        title={`filter source:${sourceName(entry.source_id)}`}
        onclick={(ev) => { ev.stopPropagation(); onSourceClick(ev, sourceName(entry.source_id)); }}>
        {sourceName(entry.source_id)}
      </button>
    {:else if c.id === "service"}
      <span class="shrink-0 truncate text-zinc-600 dark:text-zinc-400" style={c.width ? `width:${c.width}px` : ""}>
        {entry.service ?? ""}
      </span>
    {:else if c.id === "msg"}
      {@const m = msgHTML?.(entry)}
      {#if m && typeof m === "object" && "html" in m}
        <span class="flex-1 truncate" class:shrink-0={c.width > 0}
              style={c.width ? `width:${c.width}px;flex:none` : ""}>{@html m.html}</span>
      {:else}
        <span class="flex-1 truncate" class:shrink-0={c.width > 0}
              style={c.width ? `width:${c.width}px;flex:none` : ""}>{m ?? entry.msg ?? ""}</span>
      {/if}
    {:else if c.kind === "field"}
      <span class="shrink-0 truncate text-zinc-600 dark:text-zinc-400 text-[11px] mono"
            style={c.width ? `width:${c.width}px` : ""}
            title={fieldValue(c) || "—"}>
        {fieldValue(c) || "—"}
      </span>
    {/if}
  {/if}
{/each}
