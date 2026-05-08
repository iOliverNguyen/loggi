<script lang="ts">
  type Container = {
    id: string;
    names: string[];
    image: string;
    state: string;
    status: string;
    created: number;
  };

  let { container }: { container: Container } = $props();

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

<div
  class="absolute left-full top-0 ml-2 z-30 w-60 rounded shadow-lg
         bg-zinc-800 dark:bg-zinc-950 text-zinc-100 text-[11px] p-2
         border border-zinc-700 pointer-events-none">
  <div class="mono break-all leading-tight">{container.image}</div>
  <dl class="mt-1.5 grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5 text-[10px]">
    <dt class="text-zinc-400">state</dt>
    <dd class="mono">{container.state}{container.status ? ` · ${container.status}` : ""}</dd>
    <dt class="text-zinc-400">created</dt>
    <dd class="mono">{fmtCreated(container.created)}</dd>
    <dt class="text-zinc-400">id</dt>
    <dd class="mono">{container.id.slice(0, 12)}</dd>
  </dl>
</div>
