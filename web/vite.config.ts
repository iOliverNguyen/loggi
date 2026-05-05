import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { resolve } from "node:path";
import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { fileURLToPath } from "node:url";

const here = fileURLToPath(new URL(".", import.meta.url));

function resolveLoggiHTTP(): string {
  if (process.env.LOGGI_HTTP) return process.env.LOGGI_HTTP;
  try {
    const raw = readFileSync(resolve(homedir(), ".zz/loggi/runtime.json"), "utf8");
    const info = JSON.parse(raw) as { http?: string };
    if (info.http) return info.http;
  } catch {}
  // Fail loudly: a bogus port-0 target makes the proxy silently refuse
  // every request, which looks like the SPA is broken. Better to error
  // out at startup with an actionable message.
  throw new Error(
    "[vite] could not resolve loggi HTTP endpoint — start the server with " +
      "`./run server` (writes ~/.zz/loggi/runtime.json) or set LOGGI_HTTP=http://host:port",
  );
}

export default defineConfig(({ command }) => {
  // The dev proxy only matters for `vite serve`. Building the embedded
  // SPA (`vite build`) needs neither the runtime.json nor a target.
  const proxy = command === "serve"
    ? (() => {
        const target = resolveLoggiHTTP();
        return {
          "/ws": { target, ws: true },
          "/api": { target },
        };
      })()
    : undefined;
  return {
    plugins: [svelte()],
    build: {
      outDir: resolve(here, "../internal/app/spa"),
      emptyOutDir: true,
      target: "es2022",
    },
    server: { proxy },
  };
});
