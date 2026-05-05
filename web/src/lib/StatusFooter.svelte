<script lang="ts">
  import { onMount, onDestroy } from "svelte";

  let { connected, dropped } = $props<{ connected: boolean; dropped: number }>();

  type Health = {
    head: number;
    tail: number;
    rows: number;
    sources: number;
    sources_open: number;
    sessions: number;
    started_unix: number;
  };
  let health = $state<Health | null>(null);
  let lastHead = $state(0);
  let lastAt = $state(0);
  let rps = $state(0);

  let timer: number | null = null;
  async function poll() {
    try {
      const r = await fetch("/api/health");
      if (!r.ok) return;
      const j = (await r.json()) as Health;
      const now = performance.now();
      if (lastAt > 0) {
        const dt = (now - lastAt) / 1000;
        const dh = Math.max(0, j.head - lastHead);
        rps = dt > 0 ? dh / dt : 0;
      }
      lastHead = j.head;
      lastAt = now;
      health = j;
    } catch {}
  }

  onMount(() => {
    poll();
    timer = window.setInterval(poll, 2000);
  });
  onDestroy(() => {
    if (timer !== null) clearInterval(timer);
  });

  function fmtUptime(startedUnix: number): string {
    const s = Math.max(0, Math.floor(Date.now() / 1000 - startedUnix));
    if (s < 60) return `${s}s`;
    if (s < 3600) return `${Math.floor(s / 60)}m`;
    if (s < 86400) return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
    return `${Math.floor(s / 86400)}d`;
  }
  function fmtRps(n: number): string {
    if (n < 1) return n.toFixed(2);
    if (n < 10) return n.toFixed(1);
    return Math.round(n).toString();
  }
</script>

<footer class="border-t border-zinc-200 dark:border-zinc-800 px-3 py-1 text-[11px] text-zinc-500 flex items-center gap-3 mono shrink-0 bg-white/50 dark:bg-zinc-900/50">
  <span class={connected ? "text-emerald-500" : "text-red-500"}>
    {connected ? "● live" : "● disconnected"}
  </span>
  {#if health}
    <span title="rows in ring">{health.rows.toLocaleString()} rows</span>
    <span title="ingest rate">{fmtRps(rps)}/s</span>
    <span title="open / total sources">
      {health.sources_open}/{health.sources} src
    </span>
    <span title="active subscribers">{health.sessions} sub</span>
    <span title="server uptime">up {fmtUptime(health.started_unix)}</span>
    {#if dropped > 0}
      <span class="text-amber-500" title="dropped events">⚠ {dropped} dropped</span>
    {/if}
  {/if}
</footer>
