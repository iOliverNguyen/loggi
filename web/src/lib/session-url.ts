// Encodes a session config (filter, profile, paused, panel state, theme,
// columns) to a URL-safe hash and decodes it back. Used for both the Share
// button and the on-load restore that resets the address bar to "/" so
// installed PWA windows stay clean.

export interface SessionConfig {
  v: 1;
  filter?: string;
  profile?: string;
  paused?: boolean;
  theme?: "auto" | "light" | "dark";
  panel?: { seq: number };
  columns?: string[];
  selected?: number[]; // seqs of multi-selected rows
  highlight?: string;  // in-page highlight term
  density?: "compact" | "cozy" | "comfortable";
}

function b64urlEncode(s: string): string {
  const bytes = new TextEncoder().encode(s);
  let bin = "";
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
function b64urlDecode(s: string): string {
  const pad = s.length % 4 === 0 ? "" : "=".repeat(4 - (s.length % 4));
  const bin = atob(s.replace(/-/g, "+").replace(/_/g, "/") + pad);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  return new TextDecoder().decode(bytes);
}

export function encodeSession(cfg: SessionConfig): string {
  // strip undefined / empty so URLs stay short
  const slim: Record<string, unknown> = { v: 1 };
  if (cfg.filter) slim.filter = cfg.filter;
  if (cfg.profile) slim.profile = cfg.profile;
  if (cfg.paused) slim.paused = true;
  if (cfg.theme && cfg.theme !== "auto") slim.theme = cfg.theme;
  if (cfg.panel) slim.panel = cfg.panel;
  if (cfg.columns?.length) slim.columns = cfg.columns;
  if (cfg.selected?.length) slim.selected = cfg.selected;
  if (cfg.highlight) slim.highlight = cfg.highlight;
  if (cfg.density && cfg.density !== "cozy") slim.density = cfg.density;
  return b64urlEncode(JSON.stringify(slim));
}

export function decodeSession(encoded: string): SessionConfig | null {
  try {
    const parsed = JSON.parse(b64urlDecode(encoded));
    if (parsed && typeof parsed === "object" && parsed.v === 1) {
      return parsed as SessionConfig;
    }
  } catch {}
  return null;
}

// Reads location.hash for #s=... and returns the decoded config (or null).
// Does NOT touch history — caller decides whether to reset.
export function readSessionFromHash(): SessionConfig | null {
  const h = window.location.hash;
  if (!h.startsWith("#s=")) return null;
  return decodeSession(h.slice(3));
}

// Replace the address with "/" without adding a history entry.
export function clearAddress() {
  window.history.replaceState(null, "", "/");
}

// Builds a full https://host/#s=... URL for the current page.
export function shareURL(cfg: SessionConfig): string {
  return `${window.location.origin}/#s=${encodeSession(cfg)}`;
}
