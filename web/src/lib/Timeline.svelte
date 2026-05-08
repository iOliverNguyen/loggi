<script lang="ts">
  import { withTimeRange } from "./filter-dsl";

  let {
    filter,
    onApplyRange,
    live = true,
    height = 56,
  } = $props<{
    filter: string;
    onApplyRange: (lo: number | null, hi: number | null) => void;
    live?: boolean;
    height?: number;
  }>();

  type Bucket = { t: number; error: number; warn: number; info: number; debug: number; other: number };
  type HistogramResp = { bucket_seconds: number; from: number; to: number; buckets: Bucket[] };

  // Window in unix seconds: [now - WINDOW_SEC, now]. Slides while live.
  const WINDOW_SEC = 36000; // 10h
  // Aim for ~120 bars across the window — pick a bucket size that's a
  // multiple of 60s and lands close to that count.
  function pickBucketSize(span: number): number {
    const target = Math.max(60, Math.round(span / 120 / 60) * 60);
    return Math.min(3600, target);
  }

  let data = $state<HistogramResp | null>(null);
  let toEpoch = $state(Math.floor(Date.now() / 1000));
  let fromEpoch = $derived(toEpoch - WINDOW_SEC);
  let bucketSec = $derived(pickBucketSize(WINDOW_SEC));
  let svgEl: SVGSVGElement | null = $state(null);
  let svgWidth = $state(800);

  // Brush range: unix seconds. null = no brush.
  let brushLo = $state<number | null>(null);
  let brushHi = $state<number | null>(null);

  type DragMode = "new" | "translate" | "resize-lo" | "resize-hi";
  let dragMode = $state<DragMode | null>(null);
  let dragStartTime = $state(0);
  let dragStartBrush = $state<{ lo: number; hi: number } | null>(null);

  // Strip ts: from the filter we send to /api/histogram — we want the bars
  // to show all buckets in our window regardless of the brush's ts term,
  // otherwise the histogram and the brush would feed back into each other.
  let queryFilter = $derived(withTimeRange(filter, null, null));

  async function fetchData() {
    const t = Math.floor(Date.now() / 1000);
    if (live) toEpoch = t;
    const params = new URLSearchParams({
      bucket: String(bucketSec),
      from: String(toEpoch - WINDOW_SEC),
      to: String(toEpoch),
    });
    if (queryFilter) params.set("filter", queryFilter);
    try {
      const r = await fetch("/api/histogram?" + params.toString());
      if (!r.ok) return;
      data = (await r.json()) as HistogramResp;
    } catch {
      // ignore — keep prior data
    }
  }

  $effect(() => {
    fetchData();
    const interval = live ? 2000 : 10000;
    const id = setInterval(fetchData, interval);
    return () => clearInterval(id);
  });

  $effect(() => {
    // Refetch whenever the filter changes.
    void queryFilter;
    fetchData();
  });

  $effect(() => {
    if (!svgEl) return;
    const ro = new ResizeObserver((entries) => {
      for (const e of entries) svgWidth = Math.max(80, Math.floor(e.contentRect.width));
    });
    ro.observe(svgEl);
    return () => ro.disconnect();
  });

  function timeToX(t: number): number {
    if (!data) return 0;
    const span = data.to - data.from || 1;
    return ((t - data.from) / span) * svgWidth;
  }
  function xToTime(x: number): number {
    if (!data) return 0;
    const span = data.to - data.from || 1;
    return data.from + (x / svgWidth) * span;
  }
  function clientX(e: PointerEvent): number {
    const rect = svgEl?.getBoundingClientRect();
    if (!rect) return 0;
    return Math.max(0, Math.min(svgWidth, e.clientX - rect.left));
  }

  type Bar = {
    x: number;
    w: number;
    segs: Array<{ y: number; h: number; cls: string }>;
    total: number;
    t: number;
  };
  let bars = $derived.by((): Bar[] => {
    if (!data || data.buckets.length === 0) return [];
    const span = data.to - data.from || 1;
    const w = (data.bucket_seconds / span) * svgWidth;
    let max = 1;
    for (const b of data.buckets) {
      const t = b.error + b.warn + b.info + b.debug + b.other;
      if (t > max) max = t;
    }
    const out: Bar[] = [];
    for (const b of data.buckets) {
      const x = ((b.t - data.from) / span) * svgWidth;
      const total = b.error + b.warn + b.info + b.debug + b.other;
      const segs: Array<{ y: number; h: number; cls: string }> = [];
      let cursor = height;
      const layers: Array<[number, string]> = [
        [b.debug, "fill-zinc-400 dark:fill-zinc-600"],
        [b.info, "fill-sky-400 dark:fill-sky-500"],
        [b.warn, "fill-amber-400 dark:fill-amber-500"],
        [b.error, "fill-rose-500 dark:fill-rose-500"],
        [b.other, "fill-zinc-300 dark:fill-zinc-700"],
      ];
      for (const [count, cls] of layers) {
        if (count === 0) continue;
        const h = (count / max) * (height - 2);
        cursor -= h;
        segs.push({ y: cursor, h, cls });
      }
      out.push({ x, w: Math.max(1, w - 0.5), segs, total, t: b.t });
    }
    return out;
  });

  // Time axis ticks — 4 labels evenly spaced.
  let ticks = $derived.by(() => {
    if (!data) return [] as Array<{ x: number; label: string }>;
    const out: Array<{ x: number; label: string }> = [];
    for (let i = 0; i <= 4; i++) {
      const t = data.from + ((data.to - data.from) * i) / 4;
      const d = new Date(t * 1000);
      const pad = (n: number) => String(n).padStart(2, "0");
      const label = `${pad(d.getHours())}:${pad(d.getMinutes())}`;
      out.push({ x: (i / 4) * svgWidth, label });
    }
    return out;
  });

  function startDrag(e: PointerEvent) {
    if (!data) return;
    e.preventDefault();
    const x = clientX(e);
    const t = xToTime(x);
    if (brushLo != null && brushHi != null) {
      const xLo = timeToX(brushLo);
      const xHi = timeToX(brushHi);
      if (Math.abs(x - xLo) <= 4) {
        dragMode = "resize-lo";
      } else if (Math.abs(x - xHi) <= 4) {
        dragMode = "resize-hi";
      } else if (x > xLo && x < xHi) {
        dragMode = "translate";
        dragStartBrush = { lo: brushLo, hi: brushHi };
      } else {
        dragMode = "new";
        brushLo = t;
        brushHi = t;
      }
    } else {
      dragMode = "new";
      brushLo = t;
      brushHi = t;
    }
    dragStartTime = t;
    (e.target as Element).setPointerCapture?.(e.pointerId);
  }

  function moveDrag(e: PointerEvent) {
    if (!dragMode || !data) return;
    const t = xToTime(clientX(e));
    switch (dragMode) {
      case "new":
        if (t < dragStartTime) {
          brushLo = t;
          brushHi = dragStartTime;
        } else {
          brushLo = dragStartTime;
          brushHi = t;
        }
        break;
      case "resize-lo":
        brushLo = Math.min(t, brushHi ?? t);
        break;
      case "resize-hi":
        brushHi = Math.max(t, brushLo ?? t);
        break;
      case "translate":
        if (dragStartBrush) {
          const dt = t - dragStartTime;
          brushLo = dragStartBrush.lo + dt;
          brushHi = dragStartBrush.hi + dt;
        }
        break;
    }
  }

  function endDrag(_e: PointerEvent) {
    if (!dragMode) return;
    const moved = brushLo !== null && brushHi !== null && brushHi - brushLo > 1;
    dragMode = null;
    dragStartBrush = null;
    if (!moved) {
      // Treat a click without drag as "clear brush".
      brushLo = null;
      brushHi = null;
      onApplyRange(null, null);
      return;
    }
    onApplyRange(brushLo, brushHi);
  }

  function fmtRange(lo: number, hi: number): string {
    const fmt = (t: number) => {
      const d = new Date(t * 1000);
      const pad = (n: number) => String(n).padStart(2, "0");
      return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
    };
    const dur = Math.round(hi - lo);
    return `${fmt(lo)} – ${fmt(hi)}  (${dur < 60 ? `${dur}s` : dur < 3600 ? `${Math.round(dur / 60)}m` : `${(dur / 3600).toFixed(1)}h`})`;
  }
</script>

<div class="border-b border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-950">
  <div class="px-2 py-1 flex items-center gap-2 text-[10px] text-zinc-500">
    <span>timeline</span>
    {#if brushLo != null && brushHi != null}
      <span class="text-sky-700 dark:text-sky-400 font-mono">{fmtRange(brushLo, brushHi)}</span>
      <button
        type="button"
        class="ml-auto text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200"
        onclick={() => { brushLo = null; brushHi = null; onApplyRange(null, null); }}>
        clear
      </button>
    {:else}
      <span class="text-zinc-400">drag to filter by time</span>
    {/if}
  </div>
  <svg
    bind:this={svgEl}
    class="block w-full select-none cursor-crosshair"
    height={height + 14}
    role="img"
    aria-label="log volume timeline"
    onpointerdown={startDrag}
    onpointermove={moveDrag}
    onpointerup={endDrag}
    onpointercancel={endDrag}>
    <!-- bars -->
    {#each bars as b (b.t)}
      <g>
        {#each b.segs as seg}
          <rect x={b.x} y={seg.y} width={b.w} height={seg.h} class={seg.cls} />
        {/each}
        {#if b.total === 0}
          <rect x={b.x} y={height - 1} width={b.w} height={1} class="fill-zinc-200 dark:fill-zinc-800" />
        {/if}
      </g>
    {/each}

    <!-- brush -->
    {#if data && brushLo != null && brushHi != null}
      {@const xLo = timeToX(brushLo)}
      {@const xHi = timeToX(brushHi)}
      <rect
        x={Math.min(xLo, xHi)}
        y={0}
        width={Math.abs(xHi - xLo)}
        height={height}
        class="fill-sky-500/15 stroke-sky-500"
        stroke-width="1" />
      <line x1={xLo} y1={0} x2={xLo} y2={height} class="stroke-sky-600" stroke-width="2" />
      <line x1={xHi} y1={0} x2={xHi} y2={height} class="stroke-sky-600" stroke-width="2" />
    {/if}

    <!-- ticks -->
    {#each ticks as tk, i}
      <text
        x={i === 0 ? 2 : i === 4 ? svgWidth - 2 : tk.x}
        y={height + 11}
        text-anchor={i === 0 ? "start" : i === 4 ? "end" : "middle"}
        class="fill-zinc-400 dark:fill-zinc-500"
        style="font-size:10px;font-family:ui-monospace,SFMono-Regular,monospace">
        {tk.label}
      </text>
    {/each}
  </svg>
</div>
