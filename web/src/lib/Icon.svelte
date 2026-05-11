<script lang="ts">
  // A single inline-SVG icon. Names are kebab-case lucide-style; paths
  // are 24×24 stroke-1.5 so they read at small sizes with currentColor.
  // Keep the catalog tight — every entry is bytes in the bundle.

  type IconName =
    | "play" | "pause" | "trash" | "save" | "share" | "download"
    | "settings" | "sun" | "moon" | "monitor" | "help" | "search"
    | "filter" | "columns" | "rows" | "pin" | "plus" | "minus" | "x"
    | "refresh" | "copy" | "edit" | "check" | "chevron-down"
    | "chevron-right" | "star" | "star-filled" | "diff" | "link"
    | "chart-bar" | "grip" | "crosshair" | "panel-left";

  let { name, size = 16, class: cls = "" } = $props<{
    name: IconName;
    size?: number;
    class?: string;
  }>();

  const PATHS: Record<IconName, string> = {
    play: "M5 3l14 9-14 9V3z",
    pause: "M6 4h4v16H6zM14 4h4v16h-4z",
    trash: "M3 6h18M8 6V4a2 2 0 012-2h4a2 2 0 012 2v2M19 6l-1 14a2 2 0 01-2 2H8a2 2 0 01-2-2L5 6M10 11v6M14 11v6",
    save: "M19 21H5a2 2 0 01-2-2V5a2 2 0 012-2h11l5 5v11a2 2 0 01-2 2zM17 21v-8H7v8M7 3v5h8",
    share: "M4 12v8a2 2 0 002 2h12a2 2 0 002-2v-8M16 6l-4-4-4 4M12 2v13",
    download: "M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4M7 10l5 5 5-5M12 15V3",
    settings: "M12 15a3 3 0 100-6 3 3 0 000 6zM19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 11-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 11-4 0v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 11-2.83-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 110-4h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 112.83-2.83l.06.06a1.65 1.65 0 001.82.33H9a1.65 1.65 0 001-1.51V3a2 2 0 114 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 112.83 2.83l-.06.06a1.65 1.65 0 00-.33 1.82V9a1.65 1.65 0 001.51 1H21a2 2 0 110 4h-.09a1.65 1.65 0 00-1.51 1z",
    sun: "M12 17a5 5 0 100-10 5 5 0 000 10zM12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42",
    moon: "M21 12.79A9 9 0 1111.21 3 7 7 0 0021 12.79z",
    monitor: "M2 5h20v12H2zM8 21h8M12 17v4",
    help: "M9.09 9a3 3 0 015.83 1c0 2-3 3-3 3M12 17h.01M12 22a10 10 0 110-20 10 10 0 010 20z",
    search: "M11 19a8 8 0 100-16 8 8 0 000 16zM21 21l-4.35-4.35",
    filter: "M22 3H2l8 9.46V19l4 2v-8.54L22 3z",
    columns: "M3 4h18v16H3z M9 4v16 M15 4v16",
    rows: "M3 6h18M3 12h18M3 18h18",
    pin: "M12 22v-8M5 13h14l-2 -9H7l-2 9zM12 13v-11",
    plus: "M12 5v14M5 12h14",
    minus: "M5 12h14",
    x: "M18 6L6 18M6 6l12 12",
    refresh: "M23 4v6h-6M1 20v-6h6M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15",
    copy: "M20 9h-9a2 2 0 00-2 2v9a2 2 0 002 2h9a2 2 0 002-2v-9a2 2 0 00-2-2zM5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1",
    edit: "M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7M18.5 2.5a2.121 2.121 0 113 3L12 15l-4 1 1-4 9.5-9.5z",
    check: "M20 6L9 17l-5-5",
    "chevron-down": "M6 9l6 6 6-6",
    "chevron-right": "M9 6l6 6-6 6",
    star: "M12 2l3.09 6.26L22 9.27l-5 4.87L18.18 22 12 18.27 5.82 22 7 14.14 2 9.27l6.91-1.01L12 2z",
    "star-filled": "M12 2l3.09 6.26L22 9.27l-5 4.87L18.18 22 12 18.27 5.82 22 7 14.14 2 9.27l6.91-1.01L12 2z",
    diff: "M6 3v12M18 9v12M9 6l9 9M15 18l-9-9",
    link: "M10 13a5 5 0 007.54.54l3-3a5 5 0 00-7.07-7.07l-1.72 1.71M14 11a5 5 0 00-7.54-.54l-3 3a5 5 0 007.07 7.07l1.71-1.71",
    "chart-bar": "M3 21V10M9 21V4M15 21V13M21 21V7M2 21h20",
    grip: "M9 5h.01M9 12h.01M9 19h.01M15 5h.01M15 12h.01M15 19h.01",
    crosshair: "M12 2v4 M12 18v4 M2 12h4 M18 12h4 M12 18a6 6 0 100-12 6 6 0 000 12z",
    "panel-left": "M3 3h18v18H3zM9 3v18",
  };

  let path = $derived(PATHS[name as IconName]);
  let filled = $derived(name === "star-filled");
</script>

<svg
  xmlns="http://www.w3.org/2000/svg"
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill={filled ? "currentColor" : "none"}
  stroke="currentColor"
  stroke-width="1.75"
  stroke-linecap="round"
  stroke-linejoin="round"
  class={cls}
  aria-hidden="true">
  <path d={path} />
</svg>
