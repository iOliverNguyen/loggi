<script lang="ts">
  import { onMount, onDestroy, tick } from "svelte";
  import { Bus } from "./lib/ws";
  import type { Entry, SourceInfo, Profile } from "./lib/types";
  import { decodeEntry } from "./lib/types";
  import { ansiToHTML } from "./lib/ansi";
  import DetailPanel from "./lib/DetailPanel.svelte";
  import AddSourceTabs from "./lib/AddSourceTabs.svelte";
  import FilterBuilder from "./lib/FilterBuilder.svelte";
  import SidebarSection from "./lib/SidebarSection.svelte";
  import FacetPanel from "./lib/FacetPanel.svelte";
  import { SvelteMap, SvelteSet } from "svelte/reactivity";
  import SaveProfileModal from "./lib/SaveProfileModal.svelte";
  import ProfilesModal from "./lib/ProfilesModal.svelte";
  import HelpModal from "./lib/HelpModal.svelte";
  import QuickFilters from "./lib/QuickFilters.svelte";
  import StatusFooter from "./lib/StatusFooter.svelte";
  import DiffModal from "./lib/DiffModal.svelte";
  import Icon from "./lib/Icon.svelte";
  import RowContextMenu from "./lib/RowContextMenu.svelte";
  import Combobox from "./lib/Combobox.svelte";
  import FilterAutocomplete from "./lib/FilterAutocomplete.svelte";
  import SaveQuickModal from "./lib/SaveQuickModal.svelte";
  import SettingsModal from "./lib/SettingsModal.svelte";
  import {
    requestSaveQuick,
    QUICK_PROMPT,
    QUICK_CHANGED,
    persistQuickChips,
    loadQuickChips,
    setChipEnabled,
    computeEffectiveFilter,
    DEFAULT_CHIPS,
    type QuickChip,
  } from "./lib/quick-filters";
  import { parseClauses, withTimeRange, compileTsForWire, isSourceMuted, isSourceSoloed, setSourceMuted, setSourceSoloed, setBareFields } from "./lib/filter-dsl";
  import Timeline from "./lib/Timeline.svelte";
  import LogRow from "./lib/LogRow.svelte";
  import ColumnsMenu from "./lib/ColumnsMenu.svelte";
  import ColumnHeader from "./lib/ColumnHeader.svelte";
  import { dismissOnOutside } from "./lib/dismissable";
  import {
    loadColumns,
    saveColumns,
    fromProfileIDs,
    toProfileIDs,
    loadColumnsBySource,
    saveColumnsBySource,
    columnsFromIds,
    sourceKey,
    readEntryColumn,
    type Column,
    type ColumnsBySource,
  } from "./lib/columns";
  import {
    readSessionFromHash,
    clearAddress,
    shareURL,
    type SessionConfig,
  } from "./lib/session-url";

  // Debounce keystroke-driven side effects (localStorage writes). Each call
  // creates an independent debouncer with a `flush()` for beforeunload.
  const __debouncers: Array<() => void> = [];
  function makeDebounced<A extends unknown[]>(fn: (...a: A) => void, ms = 250): ((...a: A) => void) {
    let h: ReturnType<typeof setTimeout> | null = null;
    let pending: A | null = null;
    const flush = () => {
      if (h) { clearTimeout(h); h = null; }
      if (pending) { const a = pending; pending = null; fn(...a); }
    };
    __debouncers.push(flush);
    return (...args: A) => {
      pending = args;
      if (h) clearTimeout(h);
      h = setTimeout(flush, ms);
    };
  }
  const persistPendingFilter = makeDebounced((v: string) => {
    try { localStorage.setItem("loggi.pendingFilter", v); } catch {}
  });
  const persistHighlight = makeDebounced((v: string) => {
    try { localStorage.setItem("loggi.highlight", v); } catch {}
  });

  let bus: Bus | null = null;
  let connected = $state(false);
  let entries = $state<Entry[]>([]);
  let sources = $state<SourceInfo[]>([]);
  let dropped = $state(0);
  let lastError = $state<string>("");

  // UI density: row padding/leading. Persisted; defaults to "cozy".
  type Density = "compact" | "cozy" | "comfortable";
  let density = $state<Density>(
    (localStorage.getItem("loggi.density") as Density) ?? "cozy",
  );
  $effect(() => {
    try { localStorage.setItem("loggi.density", density); } catch {}
  });

  // Density change rescales scrollTop by the rowH ratio so the user's
  // currently-visible row stays put. Without this, the visible window
  // jumps to a different seq when toggling Compact/Cozy/Comfortable.
  function setDensity(d: Density) {
    if (d === density) return;
    const oldH = ROW_HEIGHT[density];
    const newH = ROW_HEIGHT[d];
    // Anchor on the row at viewport center (not list top), otherwise the
    // visible row drifts by ~clientHeight/2 * (newH/oldH - 1). Subtract
    // pinnedH symmetrically so a non-empty pinned section doesn't bias
    // the anchor by up to `pinnedEntries.length` rows.
    const stuckH = (headerEl?.offsetHeight ?? 0) + (pinnedEl?.offsetHeight ?? 0);
    const anchorIdx = listEl
      ? (listEl.scrollTop + listEl.clientHeight / 2 - stuckH) / oldH
      : 0;
    density = d;
    tick().then(() => {
      if (!listEl) return;
      const sh = (headerEl?.offsetHeight ?? 0) + (pinnedEl?.offsetHeight ?? 0);
      listEl.scrollTop = anchorIdx * newH + sh - listEl.clientHeight / 2;
    });
  }

  // In-page highlight term (separate from server-side filter). Empty string
  // = no highlighting. Substring match against msg, case-insensitive.
  let highlight = $state(localStorage.getItem("loggi.highlight") ?? "");
  let highlightBarOpen = $state(false);
  let highlightInputEl: HTMLInputElement | null = $state(null);
  $effect(() => persistHighlight(highlight));

  const SUB_ID = 1;
  const INITIAL_HISTORY = 300;
  const LOAD_MORE = 200;
  let filter = $state(localStorage.getItem("loggi.filter") ?? "");
  let pendingFilter = $state(localStorage.getItem("loggi.pendingFilter") ?? filter);
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
  let pinnedEl: HTMLElement | null = $state(null);
  let headerEl: HTMLElement | null = $state(null);
  let headerH = $state(24);
  $effect(() => {
    if (!headerEl) return;
    const ro = new ResizeObserver(() => { headerH = headerEl?.offsetHeight ?? 24; });
    ro.observe(headerEl);
    return () => ro.disconnect();
  });
  let stickToBottom = $state(true);
  let historyLoading = $state(false);
  let historyExhausted = $state(false);

  // Index-based row windowing. Rows are fixed height per density, so we can
  // compute the visible slice from scrollTop without DOM measurement.
  // Constants match the Tailwind padding classes applied to .logrow at the
  // bottom of this file (py-0/py-1/py-1.5 + text-xs ~16px line-height).
  const ROW_HEIGHT: Record<Density, number> = { compact: 18, cozy: 24, comfortable: 30 };
  const OVERSCAN = 12;
  let rowH = $derived(ROW_HEIGHT[density]);
  let viewportH = $state(0);
  let scrollTop = $state(0);
  let startIndex = $derived(Math.max(0, Math.floor(scrollTop / rowH) - OVERSCAN));
  let endIndex = $derived(
    Math.min(entries.length, Math.ceil((scrollTop + viewportH) / rowH) + OVERSCAN),
  );
  let visible = $derived(entries.slice(startIndex, endIndex));
  let topPad = $derived(startIndex * rowH);
  let bottomPad = $derived(Math.max(0, (entries.length - endIndex) * rowH));

  let showAddSource = $state(false);
  let filtersOpen = $state<boolean | undefined>(undefined);
  let sourcesOpen = $state<boolean | undefined>(undefined);
  let facetsOpen = $state<boolean | undefined>(undefined);
  let filterAddClause = $state(false);
  let showFilters = $state((localStorage.getItem("loggi.showFilters") ?? "1") !== "0");
  $effect(() => {
    try { localStorage.setItem("loggi.showFilters", showFilters ? "1" : "0"); } catch {}
  });
  let sidebarWidth = $state(parseInt(localStorage.getItem("loggi.sidebar.width") ?? "256", 10));
  let sidebarResizing = $state(false);
  $effect(() => { try { localStorage.setItem("loggi.sidebar.width", String(sidebarWidth)); } catch {} });
  $effect(() => {
    if (!sidebarResizing) return;
    const onMove = (e: PointerEvent) => { sidebarWidth = Math.max(200, Math.min(640, e.clientX)); };
    const onUp = () => { sidebarResizing = false; };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp, { once: true });
    return () => {
      window.removeEventListener("pointermove", onMove);
      // {once:true} self-detaches on fire; explicit removal covers mid-drag unmount.
      window.removeEventListener("pointerup", onUp);
    };
  });

  // Highlight regex/hit count derive after `entries` and `highlight` are
  // declared (above). Defined here because we need them late in the script.
  let highlightRe = $derived.by(() => {
    if (!highlight) return null;
    try {
      const escaped = highlight.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
      return new RegExp(`(${escaped})`, "ig");
    } catch {
      return null;
    }
  });
  function escapeHtml(s: string): string {
    return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  }
  function highlightMsg(s: string): string {
    if (!highlightRe || !s) return escapeHtml(s);
    return escapeHtml(s).replace(
      highlightRe,
      '<mark class="bg-yellow-200 dark:bg-yellow-700/60 text-inherit rounded-sm">$1</mark>',
    );
  }
  let highlightHits = $derived.by(() => {
    // Skip the O(N) scan when the bar is closed — counts aren't visible.
    if (!highlightRe || !highlightBarOpen) return 0;
    let n = 0;
    for (const e of entries) {
      const m = (e.msg ?? "").match(highlightRe);
      if (m) n += m.length;
    }
    return n;
  });

  // Live indicator: pulses when we received a non-history batch in the last
  // ~1.5s. lastLiveAt is updated in onMsg; liveTick drives a re-evaluation.
  let lastLiveAt = $state(0);
  let liveTick = $state(0);
  $effect(() => {
    const t = setInterval(() => (liveTick = Date.now()), 500);
    return () => clearInterval(t);
  });
  let isLivePulse = $derived(
    connected && !paused && stickToBottom && liveTick - lastLiveAt < 1500,
  );

  $effect(() => applyTheme(theme));
  $effect(() => persistPendingFilter(pendingFilter));

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
      if (hashCfg.selected?.length) selectedSet = new Set(hashCfg.selected);
      if (hashCfg.highlight !== undefined) highlight = hashCfg.highlight;
      if (hashCfg.density) density = hashCfg.density;
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
        // Re-establish state on reconnect: lastSentFilter is per-session,
        // so the subscribe below always carries fresh server state.
        lastSentFilter = null;
        // A fresh subscribe means the server will resend backlog; reset our
        // history-pagination state so scroll-to-bottom can fetch older rows
        // again.
        historyExhausted = false;
        historyLoading = false;
        resetFacets();
        bus!.send({
          type: "subscribe",
          subscribe: { sub_id: SUB_ID, filter: effectiveFilterFor(filter), history_n: INITIAL_HISTORY },
        });
        // Tell the server which profile we're using so it can apply the
        // per-profile sources overlay. Sent on every (re)connect so the
        // overlay is restored after the server-side state was lost (e.g.
        // server restart, idle exit/relaunch).
        if (activeProfile) {
          bus!.send({ type: "activate_profile", activate_profile: { name: activeProfile } });
        }
      }
    };
  });

  onDestroy(() => bus?.close());

  // Flush any debounced localStorage writes before the page goes away.
  // Mount-once / destroy-once concern, so onMount + cleanup is the right
  // primitive (vs. $effect, which is for reactive subscriptions).
  onMount(() => {
    const flush = () => { for (const f of __debouncers) f(); };
    window.addEventListener("beforeunload", flush);
    return () => window.removeEventListener("beforeunload", flush);
  });

  function onMsg(m: any) {
    if (m.type === "snapshot") {
      // snapshot.sources is wire.SourceEvent-shaped (source_id) — normalize.
      // Drop already-closed records: they shouldn't clutter the legend, and
      // the server keeps them around in s.srcs for diagnostic purposes.
      const visible = (m.snapshot.sources ?? []).filter((ev: any) => ev.state !== "closed");
      sources = visible.map((ev: any) => ({
        id: ev.source_id,
        kind: ev.kind,
        name: ev.name,
        mode: ev.mode ?? "",
        state: ev.state,
        detail: ev.detail,
      }));
      // Server-side recommendations attached to each snapshot source —
      // refresh the per-source map from disk via the wire. Doesn't auto-
      // install: snapshot lands on every reconnect and we'd churn columns.
      for (const ev of visible) {
        if (ev.columns && ev.columns.length > 0) {
          const key = sourceKey(ev.kind, ev.name);
          if (!columnsBySource[key]) {
            applySourceRecommendation(ev.kind, ev.name, ev.columns);
          }
        }
      }
    } else if (m.type === "source") {
      const ev = m.source;
      if (ev.state === "closed") {
        // Server confirmed removal — drop entirely so re-adding doesn't
        // produce a phantom duplicate row.
        sources = sources.filter((s) => s.id !== ev.source_id);
        const before = entries.length;
        entries = entries.filter((e) => e.source_id !== ev.source_id);
        if (entries.length !== before) rebuildFacets();
        return;
      }
      // Match optimistic placeholders (negative id) by (kind, name) so
      // they upgrade in place to the real server-assigned id.
      let idx = sources.findIndex((s) => s.id === ev.source_id);
      if (idx === -1) {
        idx = sources.findIndex((s) => s.id < 0 && s.kind === ev.kind && s.name === ev.name);
      }
      if (idx === -1) {
        sources = [
          ...sources,
          { id: ev.source_id, kind: ev.kind, name: ev.name, mode: ev.mode || "", state: ev.state, detail: ev.detail },
        ];
      } else {
        sources[idx] = {
          ...sources[idx],
          id: ev.source_id,
          state: ev.state,
          mode: ev.mode || sources[idx].mode,
          detail: ev.detail,
        };
        sources = [...sources];
      }
      if (ev.state === "error" && ev.detail) {
        lastError = `${ev.name || `#${ev.source_id}`}: ${ev.detail}`;
        setTimeout(() => (lastError = ""), 8000);
      }
      // Live recommendation: the server's sampler closed and pushed a
      // freshly-detected column set. Apply (may auto-install on first
      // run; otherwise stashes under the per-source map).
      if (ev.columns && ev.columns.length > 0) {
        applySourceRecommendation(ev.kind, ev.name, ev.columns);
      }
    } else if (m.type === "batch" && m.batch) {
      if (m.batch.gap_n) dropped += m.batch.gap_n;
      const incoming: Entry[] = (m.batch.entries ?? []).map(decodeEntry);
      // Track field names discovered from each row's nested JSON for the
      // filter builder's column dropdown.
      for (const e of incoming) {
        collectFieldPaths(e.fields);
        collectBuiltinValues(e);
      }

      const seen = new Set(entries.map((e) => e.seq));
      const fresh = incoming.filter((e) => !seen.has(e.seq));

      if (m.batch.is_history) {
        // Older rows: append at the tail (newest-at-top ordering preserved).
        // In-place mutation avoids reallocating a 50k-element array per batch.
        entries.push(...fresh.reverse());
        if (entries.length > MAX) entries.splice(0, entries.length - MAX);
        historyLoading = false;
        if (m.batch.end || fresh.length === 0) historyExhausted = true;
      } else {
        // Live + initial backlog: prepend in place.
        // Note: spread-arg limit is ~65k; safe today because server
        // batches are bounded well below that. Revisit if MAX climbs.
        const prepended = fresh.length;
        entries.unshift(...fresh.reverse());
        if (entries.length > MAX) entries.length = MAX;
        if (fresh.length > 0) lastLiveAt = Date.now();
        if (stickToBottom) {
          tick().then(() => {
            if (listEl) listEl.scrollTop = 0;
          });
        } else if (prepended > 0 && listEl) {
          // Anchor the user's currently-visible row across the prepend so
          // mid-list scrolling doesn't jump when live batches arrive.
          listEl.scrollTop += prepended * rowH;
        }
      }
    } else if (m.type === "ack") {
      // Acks are mostly informational; the source/batch events do the
      // user-visible work. Surface a brief toast for add/remove since those
      // are user-initiated and otherwise silent.
      const a = m.ack;
      if (a?.ok && a.src_id) {
        // src_id is set for both add and remove — message text would be
        // ambiguous, so we just keep this hook silent. Toast surfacing
        // happens in addSource/removeSource flows.
      }
    } else if (m.type === "err") {
      lastError = m.err?.detail || m.err?.code || "error";
      setTimeout(() => (lastError = ""), 5000);
      historyLoading = false;
    }
  }

  // discoveredFields accumulates dotted paths seen in entry.fields so the
  // filter builder can offer them as columns. Bounded to keep memory tight.
  let discoveredFields = new SvelteSet<string>();
  // fieldValues tracks distinct values seen per top-level field, keyed
  // by frequency, so the filter autocomplete can offer real values.
  // Capped at 50 values per field; eviction by lowest count.
  let fieldValues = new SvelteMap<string, SvelteMap<string, number>>();
  const VALUES_PER_FIELD = 50;
  // Outer cap: long-running sessions on heterogeneous logs would otherwise
  // accumulate every distinct top-level key ever seen. Insertion-order
  // eviction keeps the cache bounded.
  const MAX_FIELDS = 512;

  function bumpFieldValue(field: string, value: string) {
    if (!value) return;
    if (value.length > 200) return; // skip absurd payloads
    let m = fieldValues.get(field);
    if (!m) {
      if (fieldValues.size >= MAX_FIELDS) {
        // SvelteMap preserves insertion order; drop oldest.
        const firstKey = fieldValues.keys().next().value;
        if (firstKey !== undefined) fieldValues.delete(firstKey);
      }
      m = new SvelteMap();
      fieldValues.set(field, m);
    }
    m.set(value, (m.get(value) ?? 0) + 1);
    if (m.size > VALUES_PER_FIELD) {
      // Evict the least-frequent entry.
      let minKey = "";
      let minCount = Infinity;
      for (const [k, c] of m) {
        if (c < minCount) {
          minCount = c;
          minKey = k;
        }
      }
      if (minKey) m.delete(minKey);
    }
  }

  function collectFieldPaths(obj: unknown, prefix = "") {
    if (!obj || typeof obj !== "object" || Array.isArray(obj)) return;
    if (discoveredFields.size > 256) return;
    for (const [k, v] of Object.entries(obj as Record<string, unknown>)) {
      const path = prefix ? `${prefix}.${k}` : k;
      discoveredFields.add(path);
      // Record string/number values for the top-level path so
      // autocomplete can suggest them.
      if (typeof v === "string" || typeof v === "number") {
        bumpFieldValue(path, String(v));
      } else if (v && typeof v === "object" && !Array.isArray(v)) {
        collectFieldPaths(v, path);
      }
    }
  }

  function collectBuiltinValues(e: Entry) {
    if (e.service) bumpFieldValue("service", e.service);
    if (e.level) bumpFieldValue("level", e.level);
  }

  function resetFacets() {
    discoveredFields.clear();
    fieldValues.clear();
  }

  function rebuildFacets() {
    resetFacets();
    for (const e of entries) {
      collectFieldPaths(e.fields);
      collectBuiltinValues(e);
    }
  }

  function requestHistory() {
    if (!entries.length || historyLoading || historyExhausted) return;
    const oldest = entries[entries.length - 1].seq;
    historyLoading = true;
    bus?.send({
      type: "history",
      history: { sub_id: SUB_ID, before_seq: oldest, limit: LOAD_MORE },
    });
  }

  // Recent filter expressions (MRU first, deduped, capped). Surfaced by
  // FilterAutocomplete on empty focus and as a substring search while
  // typing.
  const FILTER_HISTORY_KEY = "loggi.filterHistory";
  const FILTER_HISTORY_MAX = 20;
  let filterHistory = $state<string[]>(loadFilterHistory());

  function loadFilterHistory(): string[] {
    try {
      const raw = localStorage.getItem(FILTER_HISTORY_KEY);
      if (raw) {
        const parsed = JSON.parse(raw);
        if (Array.isArray(parsed) && parsed.every((x: any) => typeof x === "string")) {
          return parsed.slice(0, FILTER_HISTORY_MAX);
        }
      }
    } catch {}
    return [];
  }

  function pushFilterHistory(expr: string) {
    // Drop any timeline-driven `ts:[…]` clauses before recording: those
    // come from brushing the timeline, not from the user typing, so they
    // pollute the dedupe and never round-trip meaningfully.
    const trimmed = withTimeRange(expr, null, null).trim();
    if (!trimmed) return;
    const next = [trimmed, ...filterHistory.filter((x) => x !== trimmed)].slice(0, FILTER_HISTORY_MAX);
    filterHistory = next;
    try {
      localStorage.setItem(FILTER_HISTORY_KEY, JSON.stringify(next));
    } catch {}
  }

  // Quick chip state mirror — kept in sync via QUICK_CHANGED so the
  // effective filter recomputes when the user toggles a pinned chip
  // anywhere (sidebar, modal, etc.).
  let quickChips = $state<QuickChip[]>(loadQuickChips());
  $effect(() => {
    const onChanged = () => (quickChips = loadQuickChips());
    window.addEventListener(QUICK_CHANGED, onChanged);
    return () => window.removeEventListener(QUICK_CHANGED, onChanged);
  });

  function effectiveFilterFor(working: string): string {
    // compileTsForWire translates any human-readable `ts:[HH:mm:ss.SSS – …]`
    // clauses back to the unix `ts:[lo..hi]` form the server's range
    // parser expects. Numeric ranges pass through unchanged.
    return compileTsForWire(computeEffectiveFilter(working, quickChips));
  }

  function applyFilter() {
    filter = pendingFilter;
    localStorage.setItem("loggi.filter", filter);
    pushFilterHistory(filter);
    sendFilter();
  }

  // sendFilter pushes the *effective* filter (pinned ANDed onto working)
  // to the server and resets the streaming view. Called by applyFilter
  // and by the pinned-chip toggle effect below.
  //
  // A `lastSentFilter` guard short-circuits back-to-back identical sends.
  // Without this, "Filter only by X" (which both persists pinned-chip
  // state AND calls applyFilter) fires two `filter` frames whose `entries
  // = []` resets race each other. Reset to null on bus reconnect so the
  // post-reconnect subscribe re-establishes state.
  let lastSentFilter: string | null = null;
  function sendFilter() {
    const eff = effectiveFilterFor(filter);
    if (eff === lastSentFilter) return;
    lastSentFilter = eff;
    bus?.send({ type: "filter", filter: { sub_id: SUB_ID, filter: eff } });
    entries = [];
    resetFacets();
    dropped = 0;
    lastError = "";
    historyExhausted = false;
    historyLoading = false;
  }

  // Re-send when pinned chip enable state changes. Skip the very first
  // run so we don't double-send alongside the bus's own initial filter.
  let chipApplyReady = false;
  $effect(() => {
    // Track the pinned-enabled signature so we only fire on real changes.
    const sig = quickChips
      .filter((c) => c.pinned)
      .map((c) => `${c.label}:${c.enabled !== false ? 1 : 0}`)
      .join("|");
    void sig;
    if (!chipApplyReady) {
      chipApplyReady = true;
      return;
    }
    sendFilter();
  });

  function selectProfile(name: string) {
    activeProfile = name;
    localStorage.setItem("loggi.profile", name);
    const p = profiles.find((x) => x.name === name);
    if (p) {
      pendingFilter = p.filter ?? "";
      if (p.columns && p.columns.length > 0) columns = fromProfileIDs(p.columns);
      applyFilter();
    }
    // Drive the server-side sources overlay. Server diffs against whatever
    // it last activated; failures show up as ack/err but don't block the
    // client-side filter/columns swap above.
    bus?.send({ type: "activate_profile", activate_profile: { name } });
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
      selected: selectedSet.size > 0 ? [...selectedSet].sort((a, b) => a - b) : undefined,
      highlight: highlight || undefined,
      density: density !== "cozy" ? density : undefined,
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
    if (/[\s:()\[\]"\\*]/.test(v)) {
      return `"${v.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
    }
    return v;
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
    if (next && next.split(/\s+/).includes(clause)) {
      applyFilter();
      return;
    }
    pendingFilter = next ? `${next} ${clause}` : clause;
    applyFilter();
  }

  function removeFilterClause(clause: string) {
    const tokens = pendingFilter.trim().split(/\s+/).filter((t) => t && t !== clause);
    pendingFilter = tokens.join(" ");
    applyFilter();
  }

  function isFilterClauseActive(clause: string): boolean {
    return pendingFilter.trim().split(/\s+/).includes(clause);
  }

  function replaceFilterClause(clause: string) {
    pendingFilter = clause;
    applyFilter();
  }

  function filterOnlyClause(clause: string) {
    // "Filter only by X": disable every pinned chip and set the working
    // filter to `clause`, so the effective filter is exactly this clause.
    // Pinned chips stay around for one-toggle restoration.
    const next = quickChips.map((c) => (c.pinned ? { ...c, enabled: false } : c));
    const hadEnabledPinned = quickChips.some((c) => c.pinned && c.enabled !== false);
    persistQuickChips(next);
    pendingFilter = clause;
    applyFilter();
    if (hadEnabledPinned) {
      flashToast("Pinned filters disabled — click amber chips to re-enable", 3000);
    }
  }

  let activeFilterFields = $derived.by(() => {
    const set = new Set<string>();
    const r = parseClauses(filter);
    if (!r.advanced) for (const c of r.clauses) set.add(c.field);
    return set;
  });
  function isPathFiltered(p: string[]): boolean {
    return activeFilterFields.has(p.join("."));
  }

  // Show the timeline strip (collapsible).
  let showTimeline = $state((localStorage.getItem("loggi.showTimeline") ?? "1") !== "0");
  $effect(() => { try { localStorage.setItem("loggi.showTimeline", showTimeline ? "1" : "0"); } catch {} });

  function applyTimeRange(lo: number | null, hi: number | null) {
    pendingFilter = withTimeRange(pendingFilter, lo, hi);
    applyFilter();
  }

  // Column configuration — persisted in localStorage; ID list also
  // round-trips through Profile.Columns when a profile is activated.
  // `columns` is the active visible set; `columnsBySource` holds per-source
  // overrides keyed by "kind:name" (server-pushed recommendations + user
  // edits). The active set falls back to baseline (loggi.columns.v1) when
  // no per-source override applies.
  let columns = $state<Column[]>(loadColumns());
  let columnsBySource = $state<ColumnsBySource>(loadColumnsBySource());
  let showColumnsMenu = $state(false);
  let hotColumns = $state<string[]>([]);
  // userCustomizedColumns: did the user ever save columns explicitly?
  // Determined once at boot. If false, we install the first source-shipped
  // recommendation as the active columns — covers the "fresh install +
  // first source added" path without surprising returning users.
  let userCustomizedColumns = $state(localStorage.getItem("loggi.columns.v1") !== null);
  $effect(() => { saveColumns(columns); });
  $effect(() => { saveColumnsBySource(columnsBySource); });
  $effect(() => {
    fetch("/api/columns").then((r) => r.json()).then((j) => {
      if (Array.isArray(j?.hot)) hotColumns = j.hot;
      if (Array.isArray(j?.well_known)) setBareFields(j.well_known);
      if (j?.by_source && typeof j.by_source === "object") {
        // Merge server-side prefs into the local map so a different
        // browser/profile picks them up. Local edits always win.
        const merged = { ...columnsBySource };
        for (const [k, ids] of Object.entries(j.by_source as Record<string, string[]>)) {
          if (!merged[k] && Array.isArray(ids) && ids.length > 0) {
            merged[k] = columnsFromIds(ids);
          }
        }
        columnsBySource = merged;
      }
    }).catch(() => {});
  });

  // applySourceRecommendation stashes a per-source recommendation under
  // its (kind, name) key. When the user has never customized columns, the
  // first arriving recommendation also becomes the active visible set —
  // so a brand-new install that just added one source shows the right
  // columns automatically rather than the generic 5-column default.
  function applySourceRecommendation(kind: string, name: string, ids: string[] | undefined) {
    if (!ids || ids.length === 0) return;
    const key = sourceKey(kind, name);
    const cols = columnsFromIds(ids);
    // Avoid noisy mutation when the recommendation matches what's stored.
    const prev = columnsBySource[key];
    if (prev && sameColumnIds(prev, cols)) return;
    columnsBySource = { ...columnsBySource, [key]: cols };
    if (!userCustomizedColumns) {
      columns = cols;
      // Subsequent recommendations won't auto-install — the user implicitly
      // "owns" these columns once they're on screen.
      userCustomizedColumns = true;
    }
  }

  function sameColumnIds(a: Column[], b: Column[]): boolean {
    if (a.length !== b.length) return false;
    for (let i = 0; i < a.length; i++) {
      if (a[i].id !== b[i].id || a[i].visible !== b[i].visible) return false;
    }
    return true;
  }

  // Refresh per-source health stats every 5s. Merges only the rate_ewma /
  // last_ingest_ts / line_count fields so client-side state mutations
  // (e.g. "closing") aren't overwritten by the poll.
  $effect(() => {
    const id = setInterval(async () => {
      try {
        const r = await fetch("/api/sources");
        if (!r.ok) return;
        const fresh = (await r.json()) as SourceInfo[];
        const byId = new Map(fresh.map((s) => [s.id, s]));
        sources = sources.map((s) => {
          const f = byId.get(s.id);
          if (!f) return s;
          return {
            ...s,
            rate_ewma: f.rate_ewma,
            last_ingest_ts: f.last_ingest_ts,
            line_count: f.line_count,
          };
        });
      } catch {}
    }, 5000);
    return () => clearInterval(id);
  });

  function sourceHealthDot(s: SourceInfo): { cls: string; title: string } {
    const last = s.last_ingest_ts ?? 0;
    if (s.state !== "open") return { cls: "bg-zinc-400", title: s.state };
    if (!last) return { cls: "bg-zinc-400", title: "no lines yet" };
    const age = Math.max(0, Date.now() / 1000 - last);
    const ago = age < 60 ? `${Math.round(age)}s` : age < 3600 ? `${Math.round(age / 60)}m` : `${(age / 3600).toFixed(1)}h`;
    if (age < 30) return { cls: "bg-emerald-500", title: `last line ${ago} ago` };
    if (age < 300) return { cls: "bg-amber-500", title: `last line ${ago} ago` };
    return { cls: "bg-rose-500", title: `last line ${ago} ago — possibly stalled` };
  }
  function fmtRate(r?: number): string {
    if (!r) return "0/s";
    if (r < 0.1) return "<0.1/s";
    if (r < 10) return r.toFixed(1) + "/s";
    return Math.round(r) + "/s";
  }

  // Mute and solo are mutually exclusive on the same source: enabling
  // one clears the other so a row can't simultaneously be hidden and
  // be the only thing shown.
  function toggleMute(name: string) {
    const muting = !isSourceMuted(pendingFilter, name);
    let expr = setSourceMuted(pendingFilter, name, muting);
    if (muting) expr = setSourceSoloed(expr, name, false);
    pendingFilter = expr;
    applyFilter();
  }
  function toggleSolo(name: string) {
    const soloing = !isSourceSoloed(pendingFilter, name);
    let expr = setSourceSoloed(pendingFilter, name, soloing);
    if (soloing) expr = setSourceMuted(expr, name, false);
    pendingFilter = expr;
    applyFilter();
  }
  function onColumnsChange(next: Column[]) { columns = next; }
  function visibleColumns(cols: Column[]): Column[] { return cols.filter((c) => c.visible); }
  let filterInputEl: HTMLInputElement | null = $state(null);
  let showHelp = $state(false);
  let helpInitialTab = $state<"keys" | "syntax" | "examples" | undefined>(undefined);
  let showSaveQuick = $state(false);
  let saveQuickExpr = $state("");
  let showSettings = $state(false);

  // Display toggles persisted across sessions.
  let showQuickBar = $state((localStorage.getItem("loggi.showQuickBar") ?? "1") !== "0");
  let showTimestamps = $state((localStorage.getItem("loggi.showTs") ?? "1") !== "0");
  $effect(() => { try { localStorage.setItem("loggi.showQuickBar", showQuickBar ? "1" : "0"); } catch {} });
  $effect(() => { try { localStorage.setItem("loggi.showTs", showTimestamps ? "1" : "0"); } catch {} });

  $effect(() => {
    const onPrompt = (e: Event) => {
      const detail = (e as CustomEvent<{ expr: string }>).detail;
      saveQuickExpr = detail?.expr ?? "";
      showSaveQuick = true;
    };
    window.addEventListener(QUICK_PROMPT, onPrompt);
    return () => window.removeEventListener(QUICK_PROMPT, onPrompt);
  });
  let showExportMenu = $state(false);
  let exportMenuEl: HTMLDivElement | null = $state(null);
  $effect(() => {
    if (!showExportMenu) return;
    return dismissOnOutside(exportMenuEl, () => (showExportMenu = false));
  });
  let showProfilesModal = $state(false);
  // Tracks whether Profiles was opened from inside Settings, so closing
  // Profiles can restore Settings rather than dropping to the main view.
  let openProfilesFromSettings = $state(false);
  const iconBtnCls = "p-1.5 rounded bg-zinc-100 dark:bg-zinc-800 hover:bg-zinc-200 dark:hover:bg-zinc-700 text-zinc-700 dark:text-zinc-300";

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

  let ctxMenu = $state<{ entry: Entry; x: number; y: number } | null>(null);
  function openContextMenu(ev: MouseEvent, entry: Entry) {
    ev.preventDefault();
    ctxMenu = { entry, x: ev.clientX, y: ev.clientY };
  }
  function copyEntryMsg(e: Entry) {
    navigator.clipboard.writeText(e.msg ?? "").catch(() => {});
    flashToast("message copied");
  }
  function copyEntryJSON(e: Entry) {
    navigator.clipboard
      .writeText(JSON.stringify({
        seq: e.seq, ts: e.ts, source_id: e.source_id, source: sourceName(e.source_id),
        level: e.level, service: e.service, msg: e.msg, fields: e.fields,
      }, null, 2))
      .catch(() => {});
    flashToast("row copied");
  }
  function toggleSelectionFor(seq: number) {
    const next = new Set(selectedSet);
    next.has(seq) ? next.delete(seq) : next.add(seq);
    selectedSet = next;
    lastClickedSeq = seq;
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
    // The selected row may not be in the DOM at all (windowed rendering),
    // so compute its position from `next * rowH` and scroll it into view.
    // The sticky pinned section sits above the windowed rows and covers
    // the first `pinnedH` pixels of the scrolled view; account for that
    // when checking whether the row is "above" the visible area.
    requestAnimationFrame(() => {
      if (!listEl) return;
      const stuckH = (headerEl?.offsetHeight ?? 0) + (pinnedEl?.offsetHeight ?? 0);
      const top = stuckH + next * rowH;
      const bot = top + rowH;
      if (top < listEl.scrollTop + stuckH) listEl.scrollTop = top - stuckH;
      else if (bot > listEl.scrollTop + listEl.clientHeight)
        listEl.scrollTop = bot - listEl.clientHeight;
    });
  }
  function onGlobalKey(e: KeyboardEvent) {
    // Always-fire bindings (work even when typing in inputs).
    if ((e.metaKey || e.ctrlKey) && e.shiftKey && (e.key === "l" || e.key === "L")) {
      e.preventDefault();
      copyShareURL();
      return;
    }
    if ((e.metaKey || e.ctrlKey) && (e.key === "f" || e.key === "F")) {
      e.preventDefault();
      highlightBarOpen = true;
      tick().then(() => {
        highlightInputEl?.focus();
        highlightInputEl?.select();
      });
      return;
    }
    if ((e.metaKey || e.ctrlKey) && (e.key === "b" || e.key === "B")) {
      e.preventDefault();
      showFilters = !showFilters;
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
    // Alt+1..9 switches profile by index (skips silently if out of range).
    if (e.altKey && /^[1-9]$/.test(e.key)) {
      const idx = parseInt(e.key, 10) - 1;
      if (idx < profiles.length) {
        e.preventDefault();
        selectProfile(profiles[idx].name);
      }
      return;
    }
    // Shift+1..9 toggles the Nth pinned chip enabled. If there are fewer
    // pinned chips than N, fall through to applying the Nth working chip.
    if (e.shiftKey && /^[!@#$%^&*(]$/.test(e.key)) {
      const idx = "!@#$%^&*(".indexOf(e.key);
      if (idx >= 0) {
        const pinned = quickChips.filter((c) => c.pinned);
        if (idx < pinned.length) {
          e.preventDefault();
          setChipEnabled(pinned[idx].label, pinned[idx].enabled === false);
          return;
        }
        const working = quickChips.filter((c) => !c.pinned);
        const wIdx = idx - pinned.length;
        if (wIdx < working.length) {
          e.preventDefault();
          quickLevel(working[wIdx].expr);
        }
      }
      return;
    }
  }

  function onScroll() {
    if (!listEl) return;
    scrollTop = listEl.scrollTop;
    stickToBottom = listEl.scrollTop < 32;
    // Bottom of the list = oldest entry. When the user scrolls near it,
    // pull older history. Threshold is generous so the next page lands
    // before they reach the literal end.
    const distFromBottom = listEl.scrollHeight - listEl.scrollTop - listEl.clientHeight;
    if (distFromBottom < 240) requestHistory();
  }

  // Track viewport height so the windowing math stays accurate across
  // window resizes and detail-panel open/close.
  $effect(() => {
    if (!listEl) return;
    viewportH = listEl.clientHeight;
    const ro = new ResizeObserver(() => {
      if (listEl) viewportH = listEl.clientHeight;
    });
    ro.observe(listEl);
    return () => ro.disconnect();
  });

  function addSource(kind: "file" | "docker", name: string, args: Record<string, unknown>) {
    // Optimistic placeholder so the sidebar reflects the click immediately.
    // Negative id distinguishes it from server-assigned rows; the real
    // `m.source` event matches by (kind, name) and replaces it.
    if (!sources.some((s) => s.kind === kind && s.name === name)) {
      const placeholderId = -Math.floor(Math.random() * 1_000_000) - 1;
      sources = [
        ...sources,
        { id: placeholderId, kind, name, mode: "", state: "connecting" },
      ];
    }
    bus?.send({
      type: "add_source",
      add_source: { kind, name, args },
    });
  }

  function removeSource(id: number) {
    // Optimistic transition: reflect the click immediately so the user sees
    // the row dim and the X button disable. The server's "closed" broadcast
    // then removes the row entirely; an err reply leaves the row dimmed
    // (the lastError toast tells the user what happened).
    sources = sources.map((s) => s.id === id ? { ...s, state: "closing" } : s);
    bus?.send({ type: "remove_source", remove_source: { source_id: id } });
  }
</script>

<div class="flex flex-col h-screen">
  <!-- PWA accent: thin gradient stripe at the very top, visible only when
       running as an installed app (display-mode: standalone). Matches the
       solid dark-navy theme_color in manifest.webmanifest so the OS title
       bar and the in-app stripe read as one continuous chrome band. -->
  <div aria-hidden="true" class="pwa-accent"></div>
  <!-- top bar -->
  <header
    class="z-20 border-b border-zinc-200 dark:border-zinc-800 px-4 py-2 flex items-center gap-2 bg-white/70 dark:bg-zinc-900/70 backdrop-blur">
    <strong class="mono">loggi</strong>
    <span
      class="relative inline-flex w-2.5 h-2.5"
      title={!connected ? "disconnected" : isLivePulse ? "live" : paused ? "paused" : "connected"}>
      {#if isLivePulse}
        <span class="absolute inline-flex w-full h-full rounded-full bg-emerald-400 opacity-75 animate-ping"></span>
      {/if}
      <span
        class={`relative inline-flex w-2.5 h-2.5 rounded-full ${
          !connected ? "bg-red-500" : paused ? "bg-amber-500" : "bg-emerald-500"
        }`}></span>
    </span>

    <div class="flex items-center gap-px">
      {#if profiles.length > 0}
        <Combobox
          items={profiles.map((p) => ({ value: p.name, label: p.name, hint: p.filter }))}
          value={activeProfile}
          placeholder="profile"
          searchPlaceholder="Search profiles…"
          title="Profile (type to search)"
          onChange={(v) => selectProfile(v)} />
        <button
          class="px-1.5 py-1 rounded bg-zinc-100 dark:bg-zinc-800 hover:bg-zinc-200 dark:hover:bg-zinc-700 text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100"
          title="Manage profiles"
          aria-label="manage profiles"
          onclick={() => (showProfilesModal = true)}>
          <Icon name="edit" size={14} />
        </button>
      {/if}
      <button
        class="px-1.5 py-1 rounded bg-zinc-100 dark:bg-zinc-800 hover:bg-zinc-200 dark:hover:bg-zinc-700 text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100"
        title="Save current filter as profile"
        aria-label="save profile"
        onclick={() => (showSaveProfile = true)}>
        <Icon name="save" size={14} />
      </button>
    </div>

    <div class="relative flex-1 min-w-0">
      <span class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-zinc-400">
        <Icon name="search" size={14} />
      </span>
      <input
        bind:this={filterInputEl}
        class="w-full bg-zinc-100 dark:bg-zinc-900 pl-8 {pendingFilter.trim() ? 'pr-8' : 'pr-3'} py-1.5 rounded mono text-sm border border-transparent focus:border-sky-500 outline-none"
        placeholder="filter — / to focus · Tab to autocomplete · ? for help"
        bind:value={pendingFilter}
        onkeydown={(e) => e.key === "Enter" && applyFilter()} />
      {#if pendingFilter.trim()}
        <button
          class="absolute right-2 top-1/2 -translate-y-1/2 text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200"
          title="Clear filter"
          aria-label="clear filter"
          onclick={() => { pendingFilter = ""; applyFilter(); filterInputEl?.focus(); }}>
          <Icon name="x" size={14} />
        </button>
      {/if}
      <FilterAutocomplete
        inputEl={filterInputEl}
        value={pendingFilter}
        {discoveredFields}
        {fieldValues}
        recentFilters={filterHistory}
        onChange={(v) => (pendingFilter = v)} />
    </div>

    <button
      class="px-3 py-1 rounded bg-sky-600 text-white text-sm hover:bg-sky-700"
      onclick={applyFilter}>Apply</button>

    <button
      class={paused
        ? "p-1.5 rounded bg-amber-500 text-white hover:bg-amber-600"
        : "p-1.5 rounded bg-emerald-600 text-white hover:bg-emerald-700"}
      title={paused ? "Paused — click to resume (Space)" : "Live — click to pause (Space)"}
      aria-label={paused ? "Resume" : "Pause"}
      aria-pressed={paused}
      onclick={togglePause}>
      <Icon name={paused ? "play" : "pause"} size={16} />
    </button>
    <button
      class={iconBtnCls}
      title="Clear log view"
      aria-label="clear"
      onclick={clear}>
      <Icon name="trash" size={16} />
    </button>
    <button
      class={iconBtnCls}
      title="Copy share URL (⌘⇧L)"
      aria-label="share"
      onclick={copyShareURL}>
      <Icon name="link" size={16} />
    </button>
    <div class="relative" bind:this={exportMenuEl}>
      <button
        class={iconBtnCls + " inline-flex items-center gap-0.5"}
        title="Export"
        aria-label="export"
        onclick={() => (showExportMenu = !showExportMenu)}>
        <Icon name="download" size={16} />
        <Icon name="chevron-down" size={12} class="opacity-60" />
      </button>
      {#if showExportMenu}
        <div
          class="absolute right-0 mt-1 w-52 rounded shadow-lg bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 z-30 text-sm"
          role="menu"
          tabindex="-1"
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
    <button
      class={showFilters
        ? "p-1.5 rounded bg-sky-600 text-white hover:bg-sky-700"
        : iconBtnCls}
      title={showFilters ? "Hide filter sidebar (⌘B)" : "Show filter sidebar (⌘B)"}
      aria-label="toggle filter sidebar"
      aria-pressed={showFilters}
      onclick={() => (showFilters = !showFilters)}>
      <Icon name="panel-left" size={16} />
    </button>
    <button
      class={showTimeline
        ? "p-1.5 rounded bg-sky-600 text-white hover:bg-sky-700"
        : iconBtnCls}
      title={showTimeline ? "Hide timeline strip" : "Show timeline strip"}
      aria-label="toggle timeline"
      aria-pressed={showTimeline}
      onclick={() => (showTimeline = !showTimeline)}>
      <Icon name="chart-bar" size={16} />
    </button>
    <button
      class={iconBtnCls}
      title="Configure columns"
      aria-label="columns"
      onclick={() => (showColumnsMenu = true)}>
      <Icon name="columns" size={16} />
    </button>
    <button
      class={iconBtnCls}
      title={`Density: ${density} (click to cycle)`}
      aria-label="cycle density"
      onclick={() => setDensity(density === "compact" ? "cozy" : density === "cozy" ? "comfortable" : "compact")}>
      <Icon name="rows" size={16} />
    </button>
    <button
      class={iconBtnCls}
      title="Settings"
      aria-label="settings"
      onclick={() => (showSettings = true)}>
      <Icon name="settings" size={16} />
    </button>
    <button
      class={iconBtnCls}
      title="Theme: {theme}"
      aria-label="cycle theme"
      onclick={() => (theme = theme === "auto" ? "light" : theme === "light" ? "dark" : "auto")}>
      <Icon name={theme === "dark" ? "moon" : theme === "light" ? "sun" : "monitor"} size={16} />
    </button>
    <button
      class={iconBtnCls}
      title="Help (?)"
      aria-label="help"
      onclick={() => (showHelp = true)}>
      <Icon name="help" size={16} />
    </button>
  </header>

  <!-- highlight bar: in-page substring highlighting (separate from server-side filter) -->
  {#if highlight || highlightBarOpen}
    <div class="px-4 py-1 border-b border-zinc-200 dark:border-zinc-800 flex items-center gap-2 text-xs bg-yellow-50 dark:bg-yellow-950/30">
      <span class="text-zinc-500">Highlight:</span>
      <input
        bind:this={highlightInputEl}
        class="flex-1 max-w-md px-2 py-0.5 rounded bg-white dark:bg-zinc-800 mono text-[11px] border border-transparent focus:border-amber-500 outline-none"
        placeholder="substring to highlight in messages"
        bind:value={highlight}
        onkeydown={(e) => {
          if (e.key === "Escape") { highlight = ""; highlightBarOpen = false; (e.target as HTMLElement).blur(); }
        }} />
      {#if highlight}
        <span class="text-zinc-500 mono text-[10px]">{highlightHits} match{highlightHits === 1 ? "" : "es"}</span>
      {/if}
      <button
        class="text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100"
        title="close"
        aria-label="close highlight"
        onclick={() => { highlight = ""; highlightBarOpen = false; }}>
        <Icon name="x" size={14} />
      </button>
    </div>
  {/if}

  {#if showQuickBar}
    <QuickFilters
      activeFilter={filter}
      currentFilter={pendingFilter}
      onApply={(expr) => quickLevel(expr)} />
  {/if}

  {#if showTimeline}
    <Timeline filter={filter} onApplyRange={applyTimeRange} live={!paused} />
  {/if}

  <!-- status row: counts and notices -->
  <div class="px-4 py-1 border-b border-zinc-200 dark:border-zinc-800 flex items-center gap-2 text-[11px] text-zinc-500">
    <span>{entries.length} rows{paused ? " · paused" : ""}{!stickToBottom ? " · scrolled" : ""}</span>
    {#if selectedSet.size > 0}
      <span class="text-amber-600 dark:text-amber-400">· {selectedSet.size} selected</span>
      <button class="text-[10px] text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200"
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
    {#if showFilters}
    <aside
      class="relative border-r border-zinc-200 dark:border-zinc-800 p-3 text-sm overflow-y-auto shrink-0"
      style="width: {sidebarWidth}px">
      <div
        role="separator"
        aria-orientation="vertical"
        class="absolute right-0 top-0 bottom-0 w-1 cursor-col-resize hover:bg-sky-500/40"
        class:bg-sky-500={sidebarResizing}
        onpointerdown={(e) => { e.preventDefault(); sidebarResizing = true; }}></div>
      <SidebarSection id="filters" label="Filters" bind:open={filtersOpen}>
        {#snippet headerExtra({ open })}
          {#if open}
            <button
              class="p-1 rounded text-zinc-500 hover:text-sky-600 dark:hover:text-sky-400 hover:bg-zinc-200 dark:hover:bg-zinc-800"
              title="Save current filter as a quick chip"
              aria-label="save quick filter"
              onclick={(e) => { e.stopPropagation(); requestSaveQuick(pendingFilter); }}>
              <Icon name="save" size={12} />
            </button>
          {/if}
          <button
            class="p-1 rounded text-zinc-500 hover:text-sky-600 dark:hover:text-sky-400 hover:bg-zinc-200 dark:hover:bg-zinc-800"
            title="Filter syntax help"
            aria-label="filter help"
            onclick={(e) => { e.stopPropagation(); helpInitialTab = "syntax"; showHelp = true; }}>
            <Icon name="help" size={12} />
          </button>
          <button
            class="p-1 rounded bg-sky-600 text-white hover:bg-sky-700"
            title="Add a filter clause"
            aria-label="add clause"
            onclick={(e) => { e.stopPropagation(); filtersOpen = true; filterAddClause = !filterAddClause; }}>
            <Icon name="plus" size={12} />
          </button>
        {/snippet}
        <FilterBuilder
          expression={filter}
          {discoveredFields}
          bind:showAdd={filterAddClause}
          onApply={(expr) => {
            pendingFilter = expr;
            applyFilter();
          }} />
      </SidebarSection>

      <SidebarSection id="facets" label="Facets" count={fieldValues.size} bind:open={facetsOpen}>
        <FacetPanel
          {fieldValues}
          {pendingFilter}
          onAddClause={addFilterClause}
          onRemoveClause={removeFilterClause}
          onReplace={filterOnlyClause}
          isClauseActive={isFilterClauseActive} />
        {#snippet alwaysShow()}
          <FacetPanel
            {fieldValues}
            {pendingFilter}
            onAddClause={addFilterClause}
            onRemoveClause={removeFilterClause}
            onReplace={filterOnlyClause}
            isClauseActive={isFilterClauseActive}
            pinnedOnly />
        {/snippet}
      </SidebarSection>

      <SidebarSection id="sources" label="Sources" count={sources.length} bind:open={sourcesOpen}>
        {#snippet headerExtra()}
          <button
            class="p-1 rounded bg-sky-600 text-white hover:bg-sky-700"
            onclick={(e) => { e.stopPropagation(); sourcesOpen = true; showAddSource = !showAddSource; }}
            title="Add source"
            aria-label="add source">
            <Icon name="plus" size={12} />
          </button>
        {/snippet}
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
        {@const health = sourceHealthDot(src)}
        {@const muted = isSourceMuted(filter, src.name)}
        {@const soloed = isSourceSoloed(filter, src.name)}
        <div class="mb-2 group relative pl-2" class:opacity-50={src.state === "closing"}>
          {#if sources.length > 1}
            <span class="absolute left-0 top-0 bottom-0 w-0.5 rounded-sm"
                  style="background-color: {sourceColor(src.id)}"
                  aria-hidden="true"></span>
          {/if}
          <div class="flex items-center justify-between gap-1">
            <span class={`shrink-0 w-1.5 h-1.5 rounded-full ${health.cls}`} title={health.title}></span>
            <div class="mono text-xs truncate flex-1" title={src.name}>{src.name}</div>
            <span class="text-[10px] text-zinc-500 mono shrink-0" title={`${src.line_count ?? 0} lines total`}>{fmtRate(src.rate_ewma)}</span>
            <button
              type="button"
              class={muted
                ? "p-0.5 rounded bg-rose-600 text-white"
                : "p-0.5 rounded opacity-0 group-hover:opacity-100 text-zinc-500 hover:bg-zinc-200 dark:hover:bg-zinc-800"}
              title={muted ? "Unmute — show rows from this source again" : "Mute — hide rows from this source"}
              aria-label={muted ? "unmute source" : "mute source"}
              aria-pressed={muted}
              onclick={() => toggleMute(src.name)}>
              <Icon name={muted ? "plus" : "minus"} size={12} />
            </button>
            <button
              type="button"
              class={soloed
                ? "p-0.5 rounded bg-sky-600 text-white"
                : "p-0.5 rounded opacity-0 group-hover:opacity-100 text-zinc-500 hover:bg-zinc-200 dark:hover:bg-zinc-800"}
              title={soloed ? "Unsolo" : "Solo — show only this source"}
              aria-label={soloed ? "unsolo source" : "solo source"}
              aria-pressed={soloed}
              onclick={() => toggleSolo(src.name)}>
              <Icon name="crosshair" size={12} />
            </button>
            <button
              type="button"
              class="p-0.5 rounded opacity-0 group-hover:opacity-100 text-zinc-500 hover:text-red-500 hover:bg-zinc-200 dark:hover:bg-zinc-800 disabled:hover:text-zinc-500 disabled:cursor-not-allowed"
              title={src.state === "closing" ? "Closing…" : "Remove"}
              aria-label="remove source"
              disabled={src.state === "closing"}
              onclick={() => removeSource(src.id)}>
              <Icon name="x" size={12} />
            </button>
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
      </SidebarSection>
    </aside>
    {/if}

    <!-- log list -->
    <main
      bind:this={listEl}
      onscroll={onScroll}
      class="flex-1 overflow-y-auto mono text-xs">
      <div bind:this={headerEl} class="sticky top-0 z-10 bg-zinc-50 dark:bg-zinc-900 border-b border-zinc-200 dark:border-zinc-800">
        <ColumnHeader {columns} {showTimestamps} onChange={onColumnsChange} />
      </div>
      {#if pinnedEntries.length > 0}
        <div bind:this={pinnedEl} class="sticky z-10 bg-amber-50 dark:bg-amber-950/60 border-b border-amber-300/40 dark:border-amber-700/40"
             style="top:{headerH}px">
          {#each pinnedEntries as p}
            <div class="relative pl-4 pr-3 py-1 hover:bg-amber-100/70 dark:hover:bg-amber-900/40 cursor-pointer flex gap-3 items-baseline"
                 role="button"
                 tabindex="0"
                 onclick={() => selectRow(p.seq)}
                 onkeydown={(ev) => ev.key === "Enter" && selectRow(p.seq)}>
              {#if sources.length > 1}
                <span class="absolute left-0 top-0 bottom-0 w-1"
                      style="background-color: {sourceColor(p.source_id)}"
                      title={sourceName(p.source_id)}></span>
              {/if}
              <span class="shrink-0 w-3 text-amber-600">📌</span>
              <LogRow
                entry={p}
                {columns}
                {showTimestamps}
                {levelClass}
                {sourceName}
                {fmtTs}
                onSourceClick={(_ev, name) => addFilterClause(`source:${quoteIfNeeded(name)}`)} />
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
      <div style="height: {topPad}px"></div>
      {#each visible as e (e.seq)}
        <div
          role="button"
          tabindex="0"
          class="logrow relative pl-4 pr-3 border-b border-zinc-100 dark:border-zinc-900 hover:bg-zinc-50 dark:hover:bg-zinc-900 cursor-pointer"
          class:py-0={density === "compact"}
          class:py-1={density === "cozy"}
          class:py-1.5={density === "comfortable"}
          class:leading-tight={density === "compact"}
          class:bg-sky-50={selectedSeq === e.seq}
          class:dark:bg-sky-950={selectedSeq === e.seq}
          class:!bg-amber-100={selectedSet.has(e.seq)}
          class:dark:!bg-amber-950={selectedSet.has(e.seq)}
          data-seq={e.seq}
          onclick={(ev) => rowClick(ev, e.seq)}
          oncontextmenu={(ev) => openContextMenu(ev, e)}
          onkeydown={(ev) => ev.key === "Enter" && selectRow(e.seq)}>
          {#if sources.length > 1}
            <span
              class="absolute left-0 top-0 bottom-0 w-1"
              style="background-color: {sourceColor(e.source_id)}"
              title={sourceName(e.source_id)}></span>
          {/if}
          <div class="flex gap-3">
            <LogRow
              entry={e}
              {columns}
              {showTimestamps}
              {levelClass}
              {sourceName}
              {fmtTs}
              onSourceClick={(_ev, name) => addFilterClause(`source:${quoteIfNeeded(name)}`)}
              msgHTML={(en) => {
                if (en.text && en.ansi) return { html: ansiToHTML(en.ansi) };
                const m = readEntryColumn(en, "msg");
                if (highlightRe) return { html: highlightMsg(m) };
                return m;
              }} />
          </div>
        </div>
      {/each}
      <div style="height: {bottomPad}px"></div>
      {#if entries.length > 0 && historyLoading}
        <div class="text-center text-[11px] text-zinc-500 py-2">Loading older…</div>
      {:else if entries.length > 0 && historyExhausted}
        <div class="text-center text-[10px] text-zinc-400 py-2">— end of history —</div>
      {/if}
    </main>

    {#if selectedEntry}
      <DetailPanel
        entry={selectedEntry}
        {sources}
        onClose={closePanel}
        onAddFilter={addFilterClause}
        onReplaceFilter={filterOnlyClause}
        {isPathFiltered} />
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

{#if showColumnsMenu}
  <ColumnsMenu
    {columns}
    {discoveredFields}
    {hotColumns}
    sourceRecommendations={columnsBySource}
    onChange={onColumnsChange}
    onClose={() => (showColumnsMenu = false)} />
{/if}

{#if showSaveProfile}
  <SaveProfileModal
    initialName={activeProfile && profiles.some((p) => p.name === activeProfile) ? "" : activeProfile}
    initialFilter={filter}
    initialColumns={toProfileIDs(columns)}
    initialCollapsed={profiles.find((p) => p.name === activeProfile)?.collapsed_fields ?? []}
    currentSources={sources.map((s) => ({ kind: s.kind, name: s.name }))}
    onClose={() => (showSaveProfile = false)}
    onSaved={async (name, path) => {
      await refreshProfiles();
      activeProfile = name;
      localStorage.setItem("loggi.profile", name);
      // Re-activate so the server picks up newly-bundled sources for the
      // just-saved profile (no-op if Sources didn't change).
      bus?.send({ type: "activate_profile", activate_profile: { name } });
      flashToast(`saved to ${path}`);
    }} />
{/if}

{#if ctxMenu}
  <RowContextMenu
    entry={ctxMenu.entry}
    sourceName={sourceName(ctxMenu.entry.source_id)}
    pinned={pinnedSeqs.includes(ctxMenu.entry.seq)}
    selected={selectedSet.has(ctxMenu.entry.seq)}
    selectionSize={selectedSet.size}
    x={ctxMenu.x}
    y={ctxMenu.y}
    onClose={() => (ctxMenu = null)}
    onAddFilter={addFilterClause}
    onReplaceFilter={replaceFilterClause}
    onFilterOnly={filterOnlyClause}
    onTogglePin={() => togglePin(ctxMenu!.entry.seq)}
    onCopyMsg={() => copyEntryMsg(ctxMenu!.entry)}
    onCopyJSON={() => copyEntryJSON(ctxMenu!.entry)}
    onSelectToggle={() => toggleSelectionFor(ctxMenu!.entry.seq)}
    onClearSelection={clearSelection}
    onDiff={openDiff}
    onOpenDetail={() => selectRow(ctxMenu!.entry.seq)} />
{/if}

{#if showHelp}
  <HelpModal
    initialTab={helpInitialTab}
    onClose={() => {
      showHelp = false;
      helpInitialTab = undefined;
    }} />
{/if}

{#if showSaveQuick}
  <SaveQuickModal
    expr={saveQuickExpr}
    onClose={() => (showSaveQuick = false)} />
{/if}

{#if showSettings}
  <SettingsModal
    {theme}
    {density}
    {showQuickBar}
    {showTimestamps}
    {showTimeline}
    {columns}
    profileNames={profiles.map((p) => p.name)}
    onChangeTheme={(t) => (theme = t)}
    onChangeDensity={(d) => setDensity(d)}
    onChangeShowQuickBar={(v) => (showQuickBar = v)}
    onChangeShowTimestamps={(v) => (showTimestamps = v)}
    onChangeShowTimeline={(v) => (showTimeline = v)}
    onChangeColumns={onColumnsChange}
    onClearHistory={() => {
      filterHistory = [];
      try { localStorage.removeItem("loggi.filterHistory"); } catch {}
    }}
    onClearQuickChips={() => persistQuickChips(DEFAULT_CHIPS)}
    onClearLocal={() => {
      try {
        for (const k of Object.keys(localStorage)) {
          if (k.startsWith("loggi.")) localStorage.removeItem(k);
        }
      } catch {}
      window.location.reload();
    }}
    onOpenProfiles={() => { openProfilesFromSettings = true; showSettings = false; showProfilesModal = true; }}
    onClose={() => (showSettings = false)} />
{/if}

{#if showProfilesModal}
  <ProfilesModal
    {profiles}
    {activeProfile}
    currentFilter={filter}
    onClose={() => {
      showProfilesModal = false;
      if (openProfilesFromSettings) {
        openProfilesFromSettings = false;
        showSettings = true;
      }
    }}
    onChanged={refreshProfiles}
    onActivate={(name) => {
      selectProfile(name);
      showProfilesModal = false;
      openProfilesFromSettings = false;
    }} />
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
