export interface Entry {
  seq: number;
  ts: number;
  source_id: number;
  level?: string;
  service?: string;
  msg?: string;
  fields?: Record<string, unknown>;
  ansi?: string;
  text?: boolean;
}

export interface SourceInfo {
  id: number;
  kind: string;
  name: string;
  mode: string;
  state: string;
  // Populated when state === "error" — the error reason emitted by the
  // server's attach goroutine (e.g. "container not found", TTY decode
  // failure, etc.).
  detail?: string;
  // Health stats refreshed by /api/sources poll. rate_ewma is lines/sec;
  // last_ingest_ts is unix seconds (0 = never). line_count is the total
  // number of lines ingested for this source since it was added.
  rate_ewma?: number;
  last_ingest_ts?: number;
  line_count?: number;
}

// SourceEventWire mirrors the Go-side wire.SourceEvent shape (snake_case
// source_id) — what arrives on the WS as snapshot.sources entries and as
// `m.source`. App.svelte normalizes this into SourceInfo.
export interface SourceEventWire {
  source_id: number;
  kind: string;
  name: string;
  state: string;
  mode?: string;
  detail?: string;
}

export interface SourceRef {
  kind: string; // "file" | "docker"
  name: string;
  args?: Record<string, unknown>;
}

export interface Profile {
  name: string;
  filter: string;
  columns?: string[];
  sources?: SourceRef[];
}

export interface ServerMsg {
  type: string;
  id?: number;
  batch?: { sub_id: number; entries: WireEntry[]; gap_n?: number; is_history?: boolean; end?: boolean };
  source?: SourceEventWire;
  ack?: { ok: boolean; ref_id?: number; sub_id?: number; src_id?: number; detail?: string };
  err?: { ref_id?: number; code: string; detail: string };
  snapshot?: { sources: SourceEventWire[]; head: number };
  pong?: { nonce: number };
}

// Go's json.RawMessage marshals as the raw JSON value; on the wire fields
// arrives as a Record<string, unknown> (or undefined if empty).
export interface WireEntry {
  seq: number;
  ts: number;
  source_id: number;
  level?: string;
  service?: string;
  msg?: string;
  fields?: Record<string, unknown>;
  ansi?: string;
  text?: boolean;
}

export function decodeEntry(e: WireEntry): Entry {
  return { ...e };
}
