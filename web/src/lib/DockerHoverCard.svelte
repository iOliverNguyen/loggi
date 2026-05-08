<script lang="ts">
  // Hover card pinned to the *anchor row's* viewport-bounding-rect, rendered
  // at the document level so it never gets clipped by the sidebar's
  // overflow:auto. Auto-flips to the left when there isn't room on the
  // right.
  import { tick } from "svelte";

  type Container = {
    id: string;
    names: string[];
    image: string;
    state: string;
    status: string;
    created: number;
  };

  let { container, anchor }: { container: Container; anchor: HTMLElement | null } = $props();

  let cardEl: HTMLDivElement | null = $state(null);
  let pos = $state({ left: 0, top: 0 });

  function place() {
    if (!cardEl || !anchor) return;
    const a = anchor.getBoundingClientRect();
    const c = cardEl.getBoundingClientRect();
    const pad = 8;
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    let left = a.right + pad;
    if (left + c.width + pad > vw) left = a.left - c.width - pad; // flip left
    if (left < pad) left = pad;
    let top = a.top;
    if (top + c.height + pad > vh) top = vh - c.height - pad;
    if (top < pad) top = pad;
    pos = { left, top };
  }

  $effect(() => {
    void anchor;
    tick().then(place);
  });

  function fmtCreated(unix: number): string {
    if (!unix) return "";
    const ms = unix * 1000;
    const diff = Date.now() - ms;
    const mins = Math.floor(diff / 60_000);
    if (mins < 1) return "just now";
    if (mins < 60) return `${mins}m ago`;
    const hrs = Math.floor(mins / 60);
    if (hrs < 24) return `${hrs}h ago`;
    const days = Math.floor(hrs / 24);
    return `${days}d ago`;
  }
</script>

<svelte:window onscroll={place} onresize={place} />

<div
  bind:this={cardEl}
  class="fixed z-50 w-60 max-w-[calc(100vw-1rem)] rounded shadow-lg
         bg-zinc-800 dark:bg-zinc-950 text-zinc-100 text-[11px] p-2
         border border-zinc-700 pointer-events-none"
  style={`left:${pos.left}px;top:${pos.top}px`}>
  <div class="mono break-all leading-tight">{container.image}</div>
  <dl class="mt-1.5 grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5 text-[10px]">
    <dt class="text-zinc-400">state</dt>
    <dd class="mono">{container.state}{container.status ? ` · ${container.status}` : ""}</dd>
    <dt class="text-zinc-400">created</dt>
    <dd class="mono">{fmtCreated(container.created)}</dd>
    <dt class="text-zinc-400">id</dt>
    <dd class="mono break-all">{container.id.slice(0, 12)}</dd>
  </dl>
</div>
