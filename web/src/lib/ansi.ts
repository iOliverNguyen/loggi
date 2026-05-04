// Minimal ANSI -> styled HTML span renderer. Handles SGR (color/bold) only.
// Output is escaped HTML; safe to inject via {@html ...}.

const ESC = "\x1b[";

export function ansiToHTML(input: string): string {
  if (!input) return "";
  let html = "";
  let cls: string[] = [];
  let i = 0;
  const flushOpen = () => {
    if (cls.length === 0) return "";
    return `<span class="${cls.join(" ")}">`;
  };
  let openSpan = "";
  while (i < input.length) {
    const esc = input.indexOf(ESC, i);
    if (esc === -1) {
      html += escapeHTML(input.slice(i));
      break;
    }
    html += escapeHTML(input.slice(i, esc));
    const m = input.slice(esc + 2).match(/^([\d;]*)([A-Za-z])/);
    if (!m) {
      i = esc + 2;
      continue;
    }
    if (m[2] === "m") {
      const codes = m[1].split(";").filter(Boolean).map((s) => parseInt(s, 10));
      // Close current span if any.
      if (openSpan) {
        html += "</span>";
        openSpan = "";
      }
      if (codes.length === 0 || codes.includes(0)) {
        cls = [];
      } else {
        for (const c of codes) {
          if (c === 1) cls.push("ansi-1");
          else if ((c >= 30 && c <= 37) || (c >= 90 && c <= 97)) {
            // Replace any prior color class.
            cls = cls.filter((x) => !x.match(/^ansi-(3\d|9\d)$/));
            cls.push(`ansi-${c}`);
          }
        }
      }
      openSpan = flushOpen();
      html += openSpan;
    }
    i = esc + 2 + m[0].length;
  }
  if (openSpan) html += "</span>";
  return html;
}

function escapeHTML(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}
