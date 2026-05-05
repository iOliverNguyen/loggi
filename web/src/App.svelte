<script lang="ts">
  import { onMount, onDestroy, tick } from "svelte";
  import { Bus } from "./lib/ws";
  import type { Entry, SourceInfo, Profile } from "./lib/types";
  import { decodeEntry } from "./lib/types";
  import { ansiToHTML } from "./lib/ansi";
  import DetailPanel from "./lib/DetailPanel.svelte";
  import AddSourceTabs from "./lib/AddSourceTabs.svelte";
  import SaveProfileModal from "./lib/SaveProfileModal.svelte";
  import StatusFooter from "./lib/StatusFooter.svelte";
  import DiffModal from "./lib/DiffModal.svelte";
  import {
    readSessionFromHash,
    clearAddress,
    shareURL,
    type SessionConfig,
  } from "./lib/session-url";

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

  let showAddSource = $state(false);

  $effect(() => applyTheme(theme));

  onMount(async () => {
    // Apply session config from URL hash, if any, then strip the hash so the
    // address bar stays clean (matters for installed-as-PWA windows).
    const hashCfg = readSessionFromHash();
    if (hashCfg) {
      if (hashCfg.filter !== undefined) {
        filter = hashCfg.filter;
        pendingFilter = hashCfg.filter;
        localStorage.setItem("loggi.filter", filter);
      }
      if (hashCfg.profile) activeProfile = hashCfg.profile;
      if (hashCfg.paused) paused = true;
      if (hashCfg.theme) theme = hashCfg.theme;
      if (hashCfg.panel?.seq !== undefined) selectedSeq = hashCfg.panel.seq;
      clearAddress();
    }

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
      // snapshot.sources is wire.SourceEvent-shaped (source_id) — normalize.
      sources = (m.snapshot.sources ?? []).map((ev: any) => ({
        id: ev.source_id,
        kind: ev.kind,
        name: ev.name,
        mode: ev.mode ?? "",
        state: ev.state,
        detail: ev.detail,
      }));
    } else if (m.type === "source") {
      const ev = m.source;
      const idx = sources.findIndex((s) => s.id === ev.source_id);
      if (idx === -1 && ev.state !== "closed") {
        sources = [
          ...sources,
          { id: ev.source_id, kind: ev.kind, name: ev.name, mode: ev.mode || "", state: ev.state, detail: ev.detail },
        ];
      } else if (idx !== -1) {
        if (ev.state === "closed") {
          sources = sources.map((s) =>
            s.id === ev.source_id ? { ...s, state: "closed", detail: ev.detail } : s,
          );
        } else {
          sources[idx] = {
            ...sources[idx],
            state: ev.state,
            mode: ev.mode || sources[idx].mode,
            detail: ev.detail,
          };
          sources = [...sources];
        }
      }
      if (ev.state === "error" && ev.detail) {
        lastError = `${ev.name || `#${ev.source_id}`}: ${ev.detail}`;
        setTimeout(() => (lastError = ""), 8000);
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

  let showSaveProfile = $state(false);

  async function refreshProfiles() {
    try {
      const r = await fetch("/api/profiles");
      if (r.ok) profiles = (await r.json()) as Profile[];
    } catch {}
  }

  let toast = $state("");
  function flashToast(s: string, ms = 1800) {
    toast = s;
    setTimeout(() => (toast = toast === s ? "" : toast), ms);
  }
  async function copyShareURL() {
    const cfg: SessionConfig = {
      v: 1,
      filter: filter || undefined,
      profile: activeProfile || undefined,
      paused: paused || undefined,
      theme: theme !== "auto" ? theme : undefined,
      panel: selectedSeq !== null ? { seq: selectedSeq } : undefined,
    };
    const url = shareURL(cfg);
    try {
      await navigator.clipboard.writeText(url);
      flashToast("URL copied");
    } catch {
      flashToast("copy failed");
    }
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

  // deterministic per-source color (golden-ratio hue rotation) for the gutter
  // stripe + the source column pill.
  function sourceColor(id: number): string {
    return `hsl(${(id * 137.508) % 360}, 60%, 50%)`;
  }
  function sourceName(id: number): string {
    return sources.find((s) => s.id === id)?.name ?? `#${id}`;
  }
  function quoteIfNeeded(v: string): string {
    return /[\s:()\[\]"]/.test(v) ? `"${v.replace(/"/g, '\\"')}"` : v;
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

  let selectedSeq = $state<number | null>(null);
  let selectedEntry = $derived(
    selectedSeq === null ? null : entries.find((x) => x.seq === selectedSeq) ?? null,
  );
  function selectRow(seq: number) {
    selectedSeq = selectedSeq === seq ? null : seq;
  }
  function closePanel() {
    selectedSeq = null;
  }
  function addFilterClause(clause: string) {
    const next = pendingFilter.trim();
    pendingFilter = next ? `${next} ${clause}` : clause;
    applyFilter();
  }
  let filterInputEl: HTMLInputElement | null = $state(null);
  let showHelp = $state(false);
  let showExportMenu = $state(false);

  let selectedSet = $state(new Set<number>());
  let lastClickedSeq = $state<number | null>(null);
  let pinnedSeqs = $state<number[]>([]);
  let pinnedEntries = $derived(
    pinnedSeqs
      .map((seq) => entries.find((x) => x.seq === seq))
      .filter((x): x is import("./lib/types").Entry => !!x),
  );
  let showDiff = $state(false);
  function togglePin(seq: number) {
    if (pinnedSeqs.includes(seq)) {
      pinnedSeqs = pinnedSeqs.filter((s) => s !== seq);
    } else if (pinnedSeqs.length < 5) {
      pinnedSeqs = [...pinnedSeqs, seq];
    } else {
      flashToast("max 5 pins");
    }
  }
  function openDiff() {
    if (selectedSet.size === 2) showDiff = true;
    else flashToast("select exactly 2 rows to diff");
  }

  function rowClick(ev: MouseEvent, seq: number) {
    if (ev.metaKey || ev.ctrlKey) {
      ev.preventDefault();
      const next = new Set(selectedSet);
      next.has(seq) ? next.delete(seq) : next.add(seq);
      selectedSet = next;
      lastClickedSeq = seq;
      return;
    }
    if (ev.shiftKey && lastClickedSeq !== null) {
      ev.preventDefault();
      const a = entries.findIndex((x) => x.seq === lastClickedSeq);
      const b = entries.findIndex((x) => x.seq === seq);
      if (a !== -1 && b !== -1) {
        const [lo, hi] = a < b ? [a, b] : [b, a];
        const next = new Set(selectedSet);
        for (let i = lo; i <= hi; i++) next.add(entries[i].seq);
        selectedSet = next;
      }
      return;
    }
    // plain click: open detail panel + clear selection set
    selectedSet = new Set();
    lastClickedSeq = seq;
    selectRow(seq);
  }
  function clearSelection() {
    selectedSet = new Set();
  }
  function copySelectionAsJSONL() {
    if (selectedSet.size === 0) return;
    const lines = entries
      .filter((e) => selectedSet.has(e.seq))
      .map((e) =>
        JSON.stringify({
          seq: e.seq,
          ts: e.ts,
          source_id: e.source_id,
          source: sourceName(e.source_id),
          level: e.level,
          service: e.service,
          msg: e.msg,
          fields: e.fields,
        }),
      );
    navigator.clipboard
      .writeText(lines.join("\n"))
      .then(() => flashToast(`copied ${selectedSet.size} rows`))
      .catch(() => flashToast("copy failed"));
  }
  function downloadExport(format: "jsonl" | "json") {
    const params = new URLSearchParams();
    params.set("format", format);
    if (filter) params.set("filter", filter);
    const url = `/api/export?${params.toString()}`;
    const a = document.createElement("a");
    a.href = url;
    a.download = `loggi-export.${format}`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    showExportMenu = false;
    flashToast("export started");
  }

  function inInput(t: EventTarget | null): boolean {
    const el = t as HTMLElement | null;
    return !!el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA");
  }
  function moveSelected(delta: number) {
    if (entries.length === 0) return;
    const idx = selectedSeq === null
      ? -1
      : entries.findIndex((x) => x.seq === selectedSeq);
    let next = idx + delta;
    if (idx === -1) next = delta > 0 ? 0 : entries.length - 1;
    next = Math.max(0, Math.min(entries.length - 1, next));
    selectedSeq = entries[next].seq;
    // best-effort scroll into view (relies on row keyed by seq)
    requestAnimationFrame(() => {
      const row = listEl?.querySelector(`[data-seq="${selectedSeq}"]`) as HTMLElement | null;
      row?.scrollIntoView({ block: "nearest" });
    });
  }
  function onGlobalKey(e: KeyboardEvent) {
    // Always-fire bindings (work even when typing in inputs).
    if ((e.metaKey || e.ctrlKey) && (e.key === "l" || e.key === "L")) {
      e.preventDefault();
      copyShareURL();
      return;
    }
    if ((e.metaKey || e.ctrlKey) && (e.key === "c" || e.key === "C") && selectedSet.size > 0) {
      // only steal ⌘C when nothing is text-selected (let normal copy work otherwise)
      const sel = window.getSelection?.();
      if (!sel || sel.toString() === "") {
        e.preventDefault();
        copySelectionAsJSONL();
        return;
      }
    }
    // Inside an input: only Esc has meaning (blur).
    if (inInput(e.target)) {
      if (e.key === "Escape") (e.target as HTMLElement).blur();
      return;
    }
    if (e.key === "Escape") {
      if (showHelp) showHelp = false;
      else if (showExportMenu) showExportMenu = false;
      else if (selectedSeq !== null) closePanel();
      return;
    }
    if (e.key === "/") {
      e.preventDefault();
      filterInputEl?.focus();
      filterInputEl?.select();
      return;
    }
    if (e.key === "?") {
      e.preventDefault();
      showHelp = !showHelp;
      return;
    }
    if (e.key === " ") {
      e.preventDefault();
      togglePause();
      return;
    }
    if (e.key === "j") {
      e.preventDefault();
      moveSelected(1);
      return;
    }
    if (e.key === "k") {
      e.preventDefault();
      moveSelected(-1);
      return;
    }
    if (e.key === "g") {
      e.preventDefault();
      if (entries.length) {
        selectedSeq = entries[0].seq;
        listEl && (listEl.scrollTop = 0);
      }
      return;
    }
    if (e.key === "G") {
      e.preventDefault();
      if (entries.length) {
        selectedSeq = entries[entries.length - 1].seq;
        listEl && (listEl.scrollTop = listEl.scrollHeight);
      }
      return;
    }
    if (e.key === "P" || e.key === "p") {
      // honor lowercase 'p' too — most users won't shift; but Space already
      // pauses, so 'p' for pin is fine since Space is the pause hotkey.
      if (e.key === "p" && (e.metaKey || e.ctrlKey || e.altKey)) return;
      if (selectedSeq !== null) {
        e.preventDefault();
        togglePin(selectedSeq);
      }
      return;
    }
    if (e.key === "d" && selectedSet.size === 2) {
      e.preventDefault();
      openDiff();
      return;
    }
  }

  function onScroll() {
    if (!listEl) return;
    stickToBottom = listEl.scrollTop < 32;
  }

  function addSource(kind: "file" | "docker", name: string, args: Record<string, unknown>) {
    bus?.send({
      type: "add_source",
      add_source: { kind, name, args },
    });
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
      bind:this={filterInputEl}
      class="flex-1 bg-zinc-100 dark:bg-zinc-900 px-3 py-1.5 rounded mono text-sm border border-transparent focus:border-sky-500 outline-none"
      placeholder='filter — / to focus · ? for hotkeys'
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
    <button
      class="px-3 py-1 rounded bg-zinc-200 dark:bg-zinc-800 text-sm"
      title="save current filter as a profile"
      onclick={() => (showSaveProfile = true)}>Save</button>
    <button
      class="px-3 py-1 rounded bg-zinc-200 dark:bg-zinc-800 text-sm"
      title="copy a shareable URL that restores this view"
      onclick={copyShareURL}>Share</button>
    <div class="relative">
      <button
        class="px-3 py-1 rounded bg-zinc-200 dark:bg-zinc-800 text-sm"
        title="export the current filter as JSON"
        onclick={() => (showExportMenu = !showExportMenu)}>Export ▾</button>
      {#if showExportMenu}
        <div
          class="absolute right-0 mt-1 w-48 rounded shadow-lg bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 z-30 text-sm"
          role="menu"
          tabindex="-1"
          onclick={(e) => e.stopPropagation()}
          onkeydown={(e) => e.key === "Escape" && (showExportMenu = false)}>
          <button class="block w-full text-left px-3 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800"
                  onclick={() => downloadExport("jsonl")}>Download .jsonl</button>
          <button class="block w-full text-left px-3 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800"
                  onclick={() => downloadExport("json")}>Download .json (array)</button>
          <div class="border-t border-zinc-200 dark:border-zinc-800 my-0.5"></div>
          <button class="block w-full text-left px-3 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800 disabled:opacity-50"
                  disabled={selectedSet.size === 0}
                  onclick={() => { copySelectionAsJSONL(); showExportMenu = false; }}>
            Copy {selectedSet.size} selected
          </button>
          <button class="block w-full text-left px-3 py-1.5 hover:bg-zinc-100 dark:hover:bg-zinc-800 disabled:opacity-50"
                  disabled={selectedSet.size !== 2}
                  onclick={() => { openDiff(); showExportMenu = false; }}>
            Diff selected (2)
          </button>
        </div>
      {/if}
    </div>
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
    {#if selectedSet.size > 0}
      <span class="text-amber-600 dark:text-amber-400">· {selectedSet.size} selected</span>
      <button class="ml-1 text-[10px] text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200"
              onclick={clearSelection}>(clear)</button>
    {/if}
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
        <AddSourceTabs
          onAdd={addSource}
          onClose={() => (showAddSource = false)} />
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
          {#if src.state === "error" && src.detail}
            <div class="text-[11px] text-red-500 mt-0.5 break-words" title={src.detail}>
              ⚠ {src.detail}
            </div>
          {/if}
        </div>
      {/each}
    </aside>

    <!-- log list -->
    <main
      bind:this={listEl}
      onscroll={onScroll}
      class="flex-1 overflow-y-auto mono text-xs">
      {#if pinnedEntries.length > 0}
        <div class="sticky top-0 z-10 bg-amber-50 dark:bg-amber-950/60 border-b border-amber-300/40 dark:border-amber-700/40">
          {#each pinnedEntries as p}
            <div class="relative pl-4 pr-3 py-1 hover:bg-amber-100/70 dark:hover:bg-amber-900/40 cursor-pointer flex gap-3"
                 role="button"
                 tabindex="0"
                 onclick={() => selectRow(p.seq)}
                 onkeydown={(ev) => ev.key === "Enter" && selectRow(p.seq)}>
              <span class="absolute left-0 top-0 bottom-0 w-1"
                    style="background-color: {sourceColor(p.source_id)}"></span>
              <span class="shrink-0 w-3 text-amber-600">📌</span>
              <span class="text-zinc-500 shrink-0 w-24">{fmtTs(p.ts)}</span>
              <span class={`shrink-0 w-12 ${levelClass(p.level)}`}>{(p.level ?? "").toUpperCase()}</span>
              <span class="shrink-0 w-24 truncate text-zinc-500 text-[11px]">{sourceName(p.source_id)}</span>
              <span class="shrink-0 w-32 truncate text-zinc-600 dark:text-zinc-400">{p.service ?? ""}</span>
              <span class="flex-1 truncate">{p.msg ?? ""}</span>
              <button class="text-amber-600 hover:text-amber-700 px-1 shrink-0"
                      title="unpin"
                      onclick={(e) => { e.stopPropagation(); togglePin(p.seq); }}>×</button>
            </div>
          {/each}
        </div>
      {/if}
      {#if entries.length === 0}
        <div class="p-8 text-zinc-500 flex flex-col items-start gap-2">
          <div>No logs yet.</div>
          <button class="px-3 py-1.5 rounded bg-sky-600 text-white text-xs hover:bg-sky-700"
                  onclick={() => (showAddSource = true)}>+ Add a source</button>
          <div class="text-[11px] text-zinc-400">
            or run <code class="mono">loggi tail file.log</code> in your terminal.
          </div>
        </div>
      {/if}
      {#each entries as e (e.seq)}
        <div
          role="button"
          tabindex="0"
          class="logrow relative pl-4 pr-3 py-1 border-b border-zinc-100 dark:border-zinc-900 hover:bg-zinc-50 dark:hover:bg-zinc-900 cursor-pointer"
          class:bg-sky-50={selectedSeq === e.seq}
          class:dark:bg-sky-950={selectedSeq === e.seq}
          class:!bg-amber-100={selectedSet.has(e.seq)}
          class:dark:!bg-amber-950={selectedSet.has(e.seq)}
          data-seq={e.seq}
          onclick={(ev) => rowClick(ev, e.seq)}
          onkeydown={(ev) => ev.key === "Enter" && selectRow(e.seq)}>
          <span
            class="absolute left-0 top-0 bottom-0 w-1"
            style="background-color: {sourceColor(e.source_id)}"></span>
          <div class="flex gap-3">
            <span class="text-zinc-500 shrink-0 w-24">{fmtTs(e.ts)}</span>
            <span class={`shrink-0 w-12 ${levelClass(e.level)}`}
              >{(e.level ?? "").toUpperCase()}</span>
            <button
              type="button"
              class="shrink-0 w-24 truncate text-zinc-500 text-[11px] text-left hover:text-sky-600 dark:hover:text-sky-400"
              title={`filter source:${sourceName(e.source_id)}`}
              onclick={(ev) => {
                ev.stopPropagation();
                addFilterClause(`source:${quoteIfNeeded(sourceName(e.source_id))}`);
              }}>{sourceName(e.source_id)}</button>
            <span class="shrink-0 w-32 truncate text-zinc-600 dark:text-zinc-400"
              >{e.service ?? ""}</span>
            {#if e.text && e.ansi}
              <span class="flex-1 truncate">{@html ansiToHTML(e.ansi)}</span>
            {:else}
              <span class="flex-1 truncate">{e.msg ?? ""}</span>
            {/if}
          </div>
        </div>
      {/each}
    </main>

    {#if selectedEntry}
      <DetailPanel
        entry={selectedEntry}
        {sources}
        onClose={closePanel}
        onAddFilter={addFilterClause} />
    {/if}
  </div>

  <StatusFooter {connected} {dropped} />
</div>

{#if showDiff}
  <DiffModal
    entries={entries.filter((e) => selectedSet.has(e.seq)).slice(0, 2)}
    onClose={() => (showDiff = false)} />
{/if}

<svelte:window onkeydown={onGlobalKey} />

{#if showSaveProfile}
  <SaveProfileModal
    initialName={activeProfile && profiles.some((p) => p.name === activeProfile) ? "" : activeProfile}
    initialFilter={filter}
    onClose={() => (showSaveProfile = false)}
    onSaved={async (name, path) => {
      await refreshProfiles();
      activeProfile = name;
      localStorage.setItem("loggi.profile", name);
      flashToast(`saved to ${path}`);
    }} />
{/if}

{#if showHelp}
  <div
    class="fixed inset-0 bg-black/40 z-40 flex items-center justify-center"
    role="button"
    tabindex="-1"
    onclick={() => (showHelp = false)}
    onkeydown={(e) => e.key === "Escape" && (showHelp = false)}>
    <div
      class="bg-white dark:bg-zinc-900 rounded-lg shadow-xl p-5 w-[460px] text-sm"
      role="dialog"
      tabindex="-1"
      onclick={(e) => e.stopPropagation()}
      onkeydown={(e) => e.stopPropagation()}>
      <div class="flex items-center justify-between mb-3">
        <h2 class="font-semibold">Keyboard shortcuts</h2>
        <button class="text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 px-2"
                onclick={() => (showHelp = false)}>×</button>
      </div>
      <ul class="space-y-1.5 mono text-xs">
        <li class="flex justify-between"><span>focus filter</span><kbd class="px-1.5 py-0.5 rounded bg-zinc-200 dark:bg-zinc-800">/</kbd></li>
        <li class="flex justify-between"><span>blur input / close panel / overlay</span><kbd class="px-1.5 py-0.5 rounded bg-zinc-200 dark:bg-zinc-800">Esc</kbd></li>
        <li class="flex justify-between"><span>row down / up</span><span><kbd class="px-1.5 py-0.5 rounded bg-zinc-200 dark:bg-zinc-800">j</kbd> <kbd class="px-1.5 py-0.5 rounded bg-zinc-200 dark:bg-zinc-800">k</kbd></span></li>
        <li class="flex justify-between"><span>jump top / bottom</span><span><kbd class="px-1.5 py-0.5 rounded bg-zinc-200 dark:bg-zinc-800">g</kbd> <kbd class="px-1.5 py-0.5 rounded bg-zinc-200 dark:bg-zinc-800">G</kbd></span></li>
        <li class="flex justify-between"><span>open detail panel</span><kbd class="px-1.5 py-0.5 rounded bg-zinc-200 dark:bg-zinc-800">Enter</kbd></li>
        <li class="flex justify-between"><span>pause / resume</span><kbd class="px-1.5 py-0.5 rounded bg-zinc-200 dark:bg-zinc-800">Space</kbd></li>
        <li class="flex justify-between"><span>copy share URL</span><kbd class="px-1.5 py-0.5 rounded bg-zinc-200 dark:bg-zinc-800">⌘L</kbd></li>
        <li class="flex justify-between"><span>copy selected rows</span><kbd class="px-1.5 py-0.5 rounded bg-zinc-200 dark:bg-zinc-800">⌘C</kbd></li>
        <li class="flex justify-between"><span>pin / unpin row</span><kbd class="px-1.5 py-0.5 rounded bg-zinc-200 dark:bg-zinc-800">p</kbd></li>
        <li class="flex justify-between"><span>diff 2 selected rows</span><kbd class="px-1.5 py-0.5 rounded bg-zinc-200 dark:bg-zinc-800">d</kbd></li>
        <li class="flex justify-between"><span>multi-select</span><span><kbd class="px-1.5 py-0.5 rounded bg-zinc-200 dark:bg-zinc-800">⌘click</kbd> <kbd class="px-1.5 py-0.5 rounded bg-zinc-200 dark:bg-zinc-800">⇧click</kbd></span></li>
        <li class="flex justify-between"><span>this overlay</span><kbd class="px-1.5 py-0.5 rounded bg-zinc-200 dark:bg-zinc-800">?</kbd></li>
      </ul>
    </div>
  </div>
{/if}

{#if toast}
  <div
    class="fixed bottom-4 left-1/2 -translate-x-1/2 px-3 py-1.5 rounded bg-zinc-900 dark:bg-zinc-100 text-white dark:text-zinc-900 text-sm shadow-lg z-50">
    {toast}
  </div>
{/if}

<style>
  /* Browser-native virtualization: rows that scroll off-screen skip
     layout/paint entirely. With 50k rows this keeps scrolling smooth. */
  :global(.logrow) {
    content-visibility: auto;
    contain-intrinsic-size: auto 24px;
  }
</style>
