// Per-machine memory of previously-used file paths and docker containers,
// stored in localStorage. Saved = explicit pin (kept until removed). Recent =
// last-used LRU, bounded.

const RECENT_CAP = 10;
const SAVED_CAP = 50;

type Kind = "file" | "docker";

const KEY_RECENT: Record<Kind, string> = {
  file: "loggi.recent.files",
  docker: "loggi.recent.containers",
};
const KEY_SAVED: Record<Kind, string> = {
  file: "loggi.saved.files",
  docker: "loggi.saved.containers",
};

function read(k: string): string[] {
  try {
    const v = localStorage.getItem(k);
    if (!v) return [];
    const parsed = JSON.parse(v);
    return Array.isArray(parsed) ? parsed.filter((x) => typeof x === "string") : [];
  } catch {
    return [];
  }
}
function write(k: string, list: string[]) {
  try {
    localStorage.setItem(k, JSON.stringify(list));
  } catch {}
}

export function recent(kind: Kind): string[] {
  return read(KEY_RECENT[kind]);
}
export function saved(kind: Kind): string[] {
  return read(KEY_SAVED[kind]);
}

export function pushRecent(kind: Kind, name: string): string[] {
  const list = [name, ...read(KEY_RECENT[kind]).filter((x) => x !== name)].slice(0, RECENT_CAP);
  write(KEY_RECENT[kind], list);
  return list;
}

export function toggleSaved(kind: Kind, name: string): string[] {
  const cur = read(KEY_SAVED[kind]);
  const next = cur.includes(name) ? cur.filter((x) => x !== name) : [name, ...cur].slice(0, SAVED_CAP);
  write(KEY_SAVED[kind], next);
  return next;
}

export function isSaved(kind: Kind, name: string): boolean {
  return read(KEY_SAVED[kind]).includes(name);
}
