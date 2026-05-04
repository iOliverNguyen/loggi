<script lang="ts">
  import { onMount, onDestroy, tick } from "svelte";
  import { Bus } from "./lib/ws";
  import type { Entry, SourceInfo, Profile } from "./lib/types";
  import { decodeEntry } from "./lib/types";
  import { ansiToHTML } from "./lib/ansi";

  let bus: Bus | null = null;
  let connected = $state(false);
  let entries = $state<Entry[]>([]);
  let sources = $state<SourceInfo[]>([]);
  let dropped = $state(0);
  let lastError = $state<string>("");

  const SUB_ID = 1;
  let filter = $state(localStorage.getItem("loggi.filter") ?? "");
  let pendingFilter = $state(filter);
  let paused = $state(false);
  let theme = $state<"auto" | "light" | "dark">(
    (localStorage.getItem("loggi.theme") as any) ?? "auto",
  );
  let profiles = $state<Profile[]>([]);
  let activeProfile = $state<string>(localStorage.getItem("loggi.profile") ?? "");

  // Increased MAX: row-level `content-visibility: auto` lets the browser
  // skip rendering off-screen rows even at 50k rows.
  const MAX = 50000;
  let listEl: HTMLElement | null = $state(null);
  let stickToBottom = $state(true);

  // Add-source modal state
  let showAddSource = $state(false);
  let addSourceKind = $state<"file" | "docker">("file");
  let addSourceName = $state("");
  let addSourceSince = $state("10m");

  $effect(() => applyTheme(theme));

  onMount(async () => {
    try {
      const r = await fetch("/api/profiles");
      if (r.ok) {
        profiles = (await r.json()) as Profile[];
        if (!activeProfile && profiles.length > 0) activeProfile = profiles[0].name;
      }
    } catch {}
    try {
      const r = await fetch("/api/config");
      if (r.ok) {
        const cfg = await r.json();
        if (theme === "auto" && cfg.theme && cfg.theme !== "auto") theme = cfg.theme;
        if (!localStorage.getItem("loggi.profile") && cfg.default_profile) {
          activeProfile = cfg.default_profile;
        }
      }
    } catch {}

    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    bus = new Bus(`${proto}//${location.host}/ws`);
    bus.onmessage = onMsg;
    bus.onstatus = (open) => {
      connected = open;
      if (open) {
        bus!.send({
          type: "subscribe",
          subscribe: { sub_id: SUB_ID, filter, history_n: 200 },
        });
      }
    };
  });

  onDestroy(() => bus?.close());

  function onMsg(m: any) {
    if (m.type === "snapshot") {
      sources = m.snapshot.sources ?? [];
    } else if (m.type === "source") {
      const ev = m.source;
      const idx = sources.findIndex((s) => s.id === ev.source_id);
      if (idx === -1 && ev.state !== "closed") {
        sources = [
          ...sources,
          { id: ev.source_id, kind: ev.kind, name: ev.name, mode: ev.mode || "", state: ev.state },
        ];
      } else if (idx !== -1) {
        if (ev.state === "closed") {
          sources = sources.map((s) =>
            s.id === ev.source_id ? { ...s, state: "closed" } : s,
          );
        } else {
          sources[idx] = { ...sources[idx], state: ev.state, mode: ev.mode || sources[idx].mode };
          sources = [...sources];
        }
      }
    } else if (m.type === "batch" && m.batch) {
      if (m.batch.gap_n) dropped += m.batch.gap_n;
      const incoming = (m.batch.entries ?? []).map(decodeEntry);
      entries = [...incoming.reverse(), ...entries].slice(0, MAX);
      if (stickToBottom) {
        tick().then(() => {
          if (listEl) listEl.scrollTop = 0;
        });
      }
    } else if (m.type === "err") {
      lastError = m.err?.detail || m.err?.code || "error";
      setTimeout(() => (lastError = ""), 5000);
    }
  }

  function applyFilter() {
    filter = pendingFilter;
    localStorage.setItem("loggi.filter", filter);
    bus?.send({ type: "filter", filter: { sub_id: SUB_ID, filter } });
    entries = [];
    dropped = 0;
    lastError = "";
  }

  function selectProfile(name: string) {
    activeProfile = name;
    localStorage.setItem("loggi.profile", name);
    const p = profiles.find((x) => x.name === name);
    if (p) {
      pendingFilter = p.filter ?? "";
      applyFilter();
    }
  }

  function quickLevel(expr: string) {
    pendingFilter = expr;
    applyFilter();
  }

  function togglePause() {
    paused = !paused;
    bus?.send({
      type: paused ? "pause" : "resume",
      [paused ? "pause" : "resume"]: { sub_id: SUB_ID },
    });
  }

  function clear() {
    entries = [];
    dropped = 0;
  }

  function applyTheme(t: typeof theme) {
    localStorage.setItem("loggi.theme", t);
    const prefersDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
    const dark = t === "dark" || (t === "auto" && prefersDark);
    document.documentElement.classList.toggle("dark", dark);
  }

  function fmtTs(ts: number) {
    if (!ts) return "";
    const d = new Date(ts * 1000);
    const pad = (n: number, w = 2) => String(n).padStart(w, "0");
    return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`;
  }

  function levelClass(l?: string) {
    switch ((l ?? "").toLowerCase()) {
      case "error":
      case "fatal":
        return "text-red-600 dark:text-red-400";
      case "warn":
      case "warning":
        return "text-amber-600 dark:text-amber-400";
      case "info":
        return "text-sky-700 dark:text-sky-400";
      case "debug":
        return "text-zinc-500 dark:text-zinc-400";
      case "trace":
        return "text-zinc-400 dark:text-zinc-500";
      default:
        return "text-zinc-500";
    }
  }

  let expanded = $state<Set<number>>(new Set());
  function toggle(seq: number) {
    if (expanded.has(seq)) expanded.delete(seq);
    else expanded.add(seq);
    expanded = new Set(expanded);
  }

  function onScroll() {
    if (!listEl) return;
    stickToBottom = listEl.scrollTop < 32;
  }

  function submitAddSource() {
    if (!addSourceName.trim()) return;
    const args: Record<string, unknown> = {};
    if (addSourceKind === "file") args.path = addSourceName.trim();
    if (addSourceKind === "docker") args.since = addSourceSince || "10m";
    bus?.send({
      type: "add_source",
      add_source: {
        kind: addSourceKind,
        name: addSourceName.trim(),
        args,
      },
    });
    showAddSource = false;
    addSourceName = "";
  }

  function removeSource(id: number) {
    bus?.send({ type: "remove_source", remove_source: { source_id: id } });
  }
</script>

<div class="flex flex-col h-screen">
  <!-- top bar -->
  <header
    class="border-b border-zinc-200 dark:border-zinc-800 px-4 py-2 flex items-center gap-2 bg-white/70 dark:bg-zinc-900/70 backdrop-blur">
    <strong class="mono">loggi</strong>
    <span
      title={connected ? "connected" : "disconnected"}
      class={connected ? "text-emerald-500" : "text-red-500"}>●</span>

    {#if profiles.length > 0}
      <select
        class="px-2 py-1 rounded bg-zinc-100 dark:bg-zinc-800 text-sm border border-transparent focus:border-sky-500 outline-none"
        value={activeProfile}
        onchange={(e) => selectProfile((e.currentTarget as HTMLSelectElement).value)}>
        {#each profiles as p}
          <option value={p.name}>{p.name}</option>
        {/each}
      </select>
    {/if}

    <input
      class="flex-1 bg-zinc-100 dark:bg-zinc-900 px-3 py-1.5 rounded mono text-sm border border-transparent focus:border-sky-500 outline-none"
      placeholder='filter — e.g. level:>=warn service:batch_worker *timeout*'
      bind:value={pendingFilter}
      onkeydown={(e) => e.key === "Enter" && applyFilter()} />

    <button
      class="px-3 py-1 rounded bg-sky-600 text-white text-sm hover:bg-sky-700"
      onclick={applyFilter}>Apply</button>
    <button
      class="px-3 py-1 rounded bg-zinc-200 dark:bg-zinc-800 text-sm"
      onclick={togglePause}>{paused ? "Resume" : "Pause"}</button>
    <button
      class="px-3 py-1 rounded bg-zinc-200 dark:bg-zinc-800 text-sm"
      onclick={clear}>Clear</button>
    <select
      class="px-2 py-1 rounded bg-zinc-200 dark:bg-zinc-800 text-sm"
      bind:value={theme}>
      <option value="auto">Auto</option>
      <option value="light">Light</option>
      <option value="dark">Dark</option>
    </select>
  </header>

  <!-- quick filters strip -->
  <div
    class="px-4 py-1.5 border-b border-zinc-200 dark:border-zinc-800 flex items-center gap-2 text-xs">
    <span class="text-zinc-500">Quick:</span>
    <button class="px-2 py-0.5 rounded bg-zinc-100 dark:bg-zinc-800 hover:bg-zinc-200" onclick={() => quickLevel("")}>all</button>
    <button class="px-2 py-0.5 rounded bg-zinc-100 dark:bg-zinc-800 hover:bg-zinc-200" onclick={() => quickLevel("level:>=info")}>info+</button>
    <button class="px-2 py-0.5 rounded bg-zinc-100 dark:bg-zinc-800 hover:bg-zinc-200" onclick={() => quickLevel("level:>=warn")}>warn+</button>
    <button class="px-2 py-0.5 rounded bg-zinc-100 dark:bg-zinc-800 hover:bg-zinc-200" onclick={() => quickLevel("level:>=error")}>error+</button>
    <span class="ml-4 text-zinc-500">{entries.length} rows{paused ? " · paused" : ""}{!stickToBottom ? " · scrolled" : ""}</span>
    {#if dropped > 0}
      <span class="text-amber-500">· {dropped} dropped</span>
    {/if}
    {#if lastError}
      <span class="text-red-500 truncate">· {lastError}</span>
    {/if}
  </div>

  <div class="flex-1 flex min-h-0">
    <!-- sidebar -->
    <aside
      class="w-64 border-r border-zinc-200 dark:border-zinc-800 p-3 text-sm overflow-y-auto">
      <div class="flex items-center justify-between mb-2">
        <h2 class="font-semibold">Sources</h2>
        <button
          class="text-xs px-2 py-0.5 rounded bg-sky-600 text-white hover:bg-sky-700"
          onclick={() => (showAddSource = !showAddSource)}
          title="Add source">+</button>
      </div>

      {#if showAddSource}
        <div class="mb-3 p-2 rounded bg-zinc-100 dark:bg-zinc-900 text-xs space-y-1.5">
          <select bind:value={addSourceKind} class="w-full px-1.5 py-1 rounded bg-white dark:bg-zinc-800">
            <option value="file">file</option>
            <option value="docker">docker</option>
          </select>
          <input
            class="w-full px-1.5 py-1 rounded bg-white dark:bg-zinc-800 mono"
            placeholder={addSourceKind === "file" ? "/path/to/log" : "container-name"}
            bind:value={addSourceName}
            onkeydown={(e) => e.key === "Enter" && submitAddSource()} />
          {#if addSourceKind === "docker"}
            <input
              class="w-full px-1.5 py-1 rounded bg-white dark:bg-zinc-800"
              placeholder="since (e.g. 10m, 1h)"
              bind:value={addSourceSince} />
          {/if}
          <div class="flex gap-1.5">
            <button
              class="flex-1 px-2 py-0.5 rounded bg-sky-600 text-white"
              onclick={submitAddSource}>Add</button>
            <button
              class="flex-1 px-2 py-0.5 rounded bg-zinc-300 dark:bg-zinc-700"
              onclick={() => (showAddSource = false)}>Cancel</button>
          </div>
        </div>
      {/if}

      {#if sources.length === 0}
        <p class="text-zinc-500 text-xs">
          No sources. Click + to add or run
          <code class="mono">loggi tail file.log</code>.
        </p>
      {/if}
      {#each sources as src}
        <div class="mb-2 group">
          <div class="flex items-center justify-between gap-1">
            <div class="mono text-xs truncate flex-1" title={src.name}>{src.name}</div>
            <button
              class="opacity-0 group-hover:opacity-100 text-xs text-zinc-500 hover:text-red-500"
              title="Remove"
              onclick={() => removeSource(src.id)}>×</button>
          </div>
          <div class="text-xs text-zinc-500">
            {src.kind} · {src.mode || "?"} ·
            <span class={src.state === "open" ? "text-emerald-500" : "text-red-500"}
              >{src.state}</span>
          </div>
        </div>
      {/each}
    </aside>

    <!-- log list -->
    <main
      bind:this={listEl}
      onscroll={onScroll}
      class="flex-1 overflow-y-auto mono text-xs">
      {#if entries.length === 0}
        <div class="p-8 text-zinc-500">No entries yet. Waiting for logs…</div>
      {/if}
      {#each entries as e (e.seq)}
        <div
          role="button"
          tabindex="0"
          class="logrow px-3 py-1 border-b border-zinc-100 dark:border-zinc-900 hover:bg-zinc-50 dark:hover:bg-zinc-900 cursor-pointer"
          onclick={() => toggle(e.seq)}
          onkeydown={(ev) => ev.key === "Enter" && toggle(e.seq)}>
          <div class="flex gap-3">
            <span class="text-zinc-500 shrink-0 w-24">{fmtTs(e.ts)}</span>
            <span class={`shrink-0 w-12 ${levelClass(e.level)}`}
              >{(e.level ?? "").toUpperCase()}</span>
            <span class="shrink-0 w-32 truncate text-zinc-600 dark:text-zinc-400"
              >{e.service ?? ""}</span>
            {#if e.text && e.ansi}
              <span class="flex-1 truncate">{@html ansiToHTML(e.ansi)}</span>
            {:else}
              <span class="flex-1 truncate">{e.msg ?? ""}</span>
            {/if}
          </div>
          {#if expanded.has(e.seq)}
            <pre
              class="mt-2 ml-24 p-2 bg-zinc-50 dark:bg-zinc-900 rounded overflow-x-auto whitespace-pre-wrap break-all text-[11px]">{JSON.stringify(
                {
                  seq: e.seq,
                  ts: e.ts,
                  level: e.level,
                  service: e.service,
                  msg: e.msg,
                  fields: e.fields,
                },
                null,
                2,
              )}</pre>
          {/if}
        </div>
      {/each}
    </main>
  </div>
</div>

<style>
  /* Browser-native virtualization: rows that scroll off-screen skip
     layout/paint entirely. With 50k rows this keeps scrolling smooth. */
  :global(.logrow) {
    content-visibility: auto;
    contain-intrinsic-size: auto 24px;
  }
</style>
