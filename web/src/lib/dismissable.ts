// Dismissable: closes a popover when the user mouses down outside of it.
//
// Usage (Svelte 5 effect):
//
//   $effect(() => {
//     if (!menuOpen) return;
//     return dismissOnOutside(menuEl, () => (menuOpen = false));
//   });
//
// The microtask defer keeps the toggle click that opened the popover from
// immediately closing it on the same mousedown.

export function dismissOnOutside(el: HTMLElement | null, close: () => void): () => void {
  if (!el) return () => {};
  let alive = true;
  const onDoc = (e: MouseEvent) => {
    if (!el.contains(e.target as Node)) close();
  };
  queueMicrotask(() => { if (alive) document.addEventListener("mousedown", onDoc); });
  return () => {
    alive = false;
    document.removeEventListener("mousedown", onDoc);
  };
}
