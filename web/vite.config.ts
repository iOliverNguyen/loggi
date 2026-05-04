import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { resolve } from "node:path";

export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: resolve(__dirname, "../internal/app/spa"),
    emptyOutDir: true,
    target: "es2022",
  },
  server: {
    proxy: {
      "/ws": { target: "http://127.0.0.1:61728", ws: true },
      "/api": { target: "http://127.0.0.1:61728" },
    },
  },
});
