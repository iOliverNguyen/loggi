<script lang="ts">
  import type { Entry, SourceInfo } from "./types";
  import { ansiToHTML } from "./ansi";
  import JsonTree from "./JsonTree.svelte";

  let { entry, sources, onClose, onAddFilter } = $props<{
    entry: Entry;
    sources: SourceInfo[];
    onClose: () => void;
    onAddFilter: (clause: string) => void;
  }>();

  let width = $state(parseInt(localStorage.getItem("loggi.panel.width") ?? "480", 10));
  let resizing = $state(false);

  function startResize(e: PointerEvent) {
    resizing = true;
    e.preventDefault();
  }

  $effect(() => {
    if (!resizing) return;
    const onMove = (e: PointerEvent) => {
      width = Math.max(320, Math.min(900, window.innerWidth - e.clientX));
    };
    const onUp = () => {
      resizing = false;
      localStorage.setItem("loggi.panel.width", String(width));
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    return () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
  });

  function fmtTs(ts: number) {
    if (!ts) return "";
    const d = new Date(ts * 1000);
    const pad = (n: number, w = 2) => String(n).padStart(w, "0");
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ` +
      `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`;
  }
  function levelClass(l?: string) {
    switch ((l ?? "").toLowerCase()) {
      case "error":
      case "fatal":
        return "bg-red-600/10 text-red-700 dark:text-red-400";
      case "warn":
      case "warning":
        return "bg-amber-600/10 text-amber-700 dark:text-amber-400";
      case "info":
        return "bg-sky-600/10 text-sky-700 dark:text-sky-400";
      case "debug":
        return "bg-zinc-500/10 text-zinc-500";
      case "trace":
        return "bg-zinc-400/10 text-zinc-400";
      default:
        return "bg-zinc-500/10 text-zinc-500";
    }
  }

  function srcOf(id: number): SourceInfo | undefined {
    return sources.find((s) => s.id === id);
  }

  function valueLiteral(v: unknown): string {
    if (typeof v === "string") {
      return /[\s:()\[\]"]/.test(v) ? `"${v.replace(/"/g, '\\"')}"` : v;
    }
    return String(v);
  }
  function fieldRef(p: string[]): string {
    if (p.length === 1) return p[0];
    return "@" + p.join(".");
  }

  function onAddField(path: string[], v: unknown, negate: boolean) {
    const clause = `${negate ? "-" : ""}${fieldRef(path)}:${valueLiteral(v)}`;
    onAddFilter(clause);
  }

  function copyJSON() {
    const obj = {
      seq: entry.seq,
      ts: entry.ts,
      source_id: entry.source_id,
      level: entry.level,
      service: entry.service,
      msg: entry.msg,
      fields: entry.fields,
    };
    navigator.clipboard.writeText(JSON.stringify(obj, null, 2)).catch(() => {});
  }
  function copyMsg() {
    navigator.clipboard.writeText(entry.msg ?? "").catch(() => {});
  }
  function showSourceFilter() {
    const s = srcOf(entry.source_id);
    if (s) onAddFilter(`source:${valueLiteral(s.name)}`);
  }
  function showTrace() {
    const t = (entry.fields as any)?.trace_id;
    if (typeof t === "string" || typeof t === "number") {
      onAddFilter(`trace_id:${valueLiteral(t)}`);
    }
  }

  let traceID = $derived((entry.fields as any)?.trace_id);
  let src = $derived(srcOf(entry.source_id));
</script>

<aside
  class="border-l border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-950 flex flex-col shrink-0 relative"
  style="width: {width}px">
  <!-- resize handle -->
  <div
    role="separator"
    aria-orientation="vertical"
    class="absolute left-0 top-0 bottom-0 w-1 cursor-col-resize hover:bg-sky-500/40"
    class:bg-sky-500={resizing}
    onpointerdown={startResize}></div>

  <!-- header -->
  <div class="px-3 py-2 border-b border-zinc-200 dark:border-zinc-800 flex items-center gap-2">
    <span class={`px-1.5 py-0.5 rounded text-[10px] font-bold uppercase ${levelClass(entry.level)}`}>
      {entry.level || "—"}
    </span>
    <span class="text-xs text-zinc-500 mono">{fmtTs(entry.ts)}</span>
    <span class="text-xs text-zinc-700 dark:text-zinc-300 truncate flex-1" title={entry.service}>
      {entry.service ?? ""}
    </span>
    <button
      class="text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 px-2"
      title="close (Esc)"
      onclick={onClose}>×</button>
  </div>

  <div class="flex-1 overflow-y-auto p-3 text-xs space-y-4">
    <!-- message -->
    <section>
      <div class="flex items-center justify-between mb-1">
        <h3 class="text-[10px] uppercase tracking-wider text-zinc-500 font-semibold">Message</h3>
        <button
          class="text-[10px] px-1.5 py-0.5 rounded bg-zinc-100 dark:bg-zinc-800 hover:bg-zinc-200 dark:hover:bg-zinc-700"
          title="copy message"
          onclick={copyMsg}>copy</button>
      </div>
      <div class="font-mono whitespace-pre-wrap break-words bg-zinc-50 dark:bg-zinc-900 rounded p-2">
        {#if entry.text && entry.ansi}
          {@html ansiToHTML(entry.ansi)}
        {:else}
          {entry.msg ?? ""}
        {/if}
      </div>
    </section>

    <!-- source -->
    {#if src}
      <section>
        <h3 class="text-[10px] uppercase tracking-wider text-zinc-500 font-semibold mb-1">Source</h3>
        <div class="bg-zinc-50 dark:bg-zinc-900 rounded p-2 space-y-1">
          <div class="flex items-center gap-2">
            <span class="text-zinc-500 w-12 shrink-0">name</span>
            <span class="mono">{src.name}</span>
          </div>
          <div class="flex items-center gap-2">
            <span class="text-zinc-500 w-12 shrink-0">kind</span>
            <span>{src.kind}</span>
          </div>
          <div class="flex items-center gap-2">
            <span class="text-zinc-500 w-12 shrink-0">state</span>
            <span class={src.state === "open" ? "text-emerald-500" : "text-red-500"}>{src.state}</span>
            <span class="text-zinc-400">· {src.mode || "?"}</span>
          </div>
          <button
            class="mt-1 text-[10px] px-1.5 py-0.5 rounded bg-sky-600/10 text-sky-700 dark:text-sky-400 hover:bg-sky-600/20"
            onclick={showSourceFilter}>filter to this source</button>
        </div>
      </section>
    {/if}

    <!-- trace -->
    {#if traceID !== undefined && traceID !== null}
      <section>
        <h3 class="text-[10px] uppercase tracking-wider text-zinc-500 font-semibold mb-1">Trace</h3>
        <div class="bg-zinc-50 dark:bg-zinc-900 rounded p-2 flex items-center justify-between gap-2">
          <span class="mono break-all">{String(traceID)}</span>
          <button
            class="text-[10px] px-1.5 py-0.5 rounded bg-sky-600/10 text-sky-700 dark:text-sky-400 hover:bg-sky-600/20 shrink-0"
            onclick={showTrace}>show all</button>
        </div>
      </section>
    {/if}

    <!-- fields -->
    {#if entry.fields && Object.keys(entry.fields).length > 0}
      <section>
        <h3 class="text-[10px] uppercase tracking-wider text-zinc-500 font-semibold mb-1">Fields</h3>
        <div class="bg-zinc-50 dark:bg-zinc-900 rounded p-2 overflow-x-auto">
          <JsonTree value={entry.fields} onAddFilter={onAddField} depth={1} />
        </div>
      </section>
    {/if}

    <!-- raw -->
    <section>
      <div class="flex items-center justify-between mb-1">
        <h3 class="text-[10px] uppercase tracking-wider text-zinc-500 font-semibold">Raw</h3>
        <button
          class="text-[10px] px-1.5 py-0.5 rounded bg-zinc-100 dark:bg-zinc-800 hover:bg-zinc-200 dark:hover:bg-zinc-700"
          onclick={copyJSON}>copy</button>
      </div>
      <pre
        class="font-mono text-[11px] whitespace-pre-wrap break-all bg-zinc-50 dark:bg-zinc-900 rounded p-2 max-h-64 overflow-y-auto">{JSON.stringify(
          {
            seq: entry.seq,
            ts: entry.ts,
            source_id: entry.source_id,
            level: entry.level,
            service: entry.service,
            msg: entry.msg,
            fields: entry.fields,
          },
          null,
          2,
        )}</pre>
    </section>

    <!-- meta footer -->
    <div class="text-[10px] text-zinc-400 pt-2 border-t border-zinc-200 dark:border-zinc-800">
      seq #{entry.seq} · source #{entry.source_id}
    </div>
  </div>
</aside>
