# loggi — local-first log viewer

A single-binary log viewer for developers. Runs a local server and serves a
Svelte web UI that streams JSON-structured and ANSI-colored logs from files,
stdin, or Docker containers. Multiple clients attach to the same server; the
server auto-starts on demand.

https://github.com/user-attachments/assets/681a7ae0-4024-45a8-be4a-7f15d4fede36

## Features

- **Single self-contained binary** — Go server with the Svelte SPA embedded;
  no external services, no network egress. Talks to clients over a per-user
  Unix socket.
- **Auto-starting daemon** — `loggi tail`, `docker`, `stdin`, and `web`
  spawn the server on demand (flock-guarded) and reuse it across terminals.
  Multiple clients can attach to the same running server.
- **Multi-source ingest** — tail files, stream `docker logs`, or pipe
  arbitrary stdin. Each source is colour-striped, mute/solo-able, and can
  be added or removed live from the UI.
- **JSON + ANSI parsing** — structured fields surface as facets and
  columns; non-JSON prelude lines are kept as plain messages with ANSI
  colour preserved.
- **Datadog-style filter DSL** — `level:error`, `service:a OR service:b`,
  `@nested.path:>=50`, `*substring*`, `/regex/`, ranges, negation,
  implicit AND, parens. Compiled filters short-circuit on column
  dictionaries before touching rows.
- **Facets sidebar** — field keys ranked by duplication, click-to-filter
  values with occurrence counts, pinnable keys/values, per-key search when 
  more than 10 distinct values.
- **Pinned filter chips** — keep frequently-used clauses one click away;
  they persist across sessions and AND onto the working filter.
- **Virtualised streaming view** — prepend on live tail, infinite scroll
  back through history, row pin + side-by-side diff of any two rows.
- **Detail panel** with a JSON tree where every leaf has `[+]` / `[•]` /
  `[-]` buttons to add, replace, or exclude that `field:value` from the
  filter.
- **Resizable, reorderable columns** — drag headers to reorder, drag edges
  to resize, add custom `@dotted.path` columns. Timeline strip supports
  brush-to-zoom into a time range.
- **Profiles** — save filter + columns + sources as a named profile;
  merged from `~/.zz/loggi/profiles.toml` and repo-local files.
- **Repo-aware config** — global → repo (`.loggi/config.toml` or
  `cmd/.config/loggi.toml`) → `*.local.toml` precedence, with
  `loggi config print` showing the merged view.
- **Memory-efficient storage** — ring buffer (524288 entries default),
  per-field hot columns with promotion, global string interner with
  refcounts, content-addressed blob slab so a repeated 30 KB config dump
  is stored once across thousands of log lines.

<img width="1800" height="1038" alt="Image" src="https://github.com/user-attachments/assets/1653e204-94a9-4308-ba2f-8c344501b1d6" />

<img width="1800" height="1038" alt="Image" src="https://github.com/user-attachments/assets/653890e5-6014-4f12-a4ec-a0ec0da80d08" />

<img width="1800" height="1038" alt="Image" src="https://github.com/user-attachments/assets/c120ec94-3c35-4a01-adc5-a58655ede886" />

<img width="1800" height="1038" alt="Image" src="https://github.com/user-attachments/assets/3d5956d8-2c5f-4ced-af62-41d9a3844279" />

## Install

From a fresh clone:

```sh
./run build-all     # checks toolchain, builds SPA + loggi binary
```

Requires Go 1.26+, Node 20+, and pnpm (`npm install -g pnpm` or
`corepack enable`). The resulting `./loggi` binary is self-contained
(SPA is embedded).

Or build the pieces manually:

```sh
# 1. Build the Svelte SPA (output goes to internal/app/spa/)
cd web && pnpm install && pnpm run build && cd ..

# 2. Build the Go binary (embeds the SPA)
go build -o loggi ./cmd/loggi
```

Other useful dispatcher commands:

```sh
./run build         # incremental SPA + Go binary
./run dev-web       # Vite dev server with HMR (proxies /ws + /api)
./run server-debug  # run the server with --debug for /api/debug/*
./run help          # list every subcommand
```

## Quick Usage

```sh
loggi
```

This will start `loggi`, open a browser window, and ready to use! 🚀🚀

Then you can add docker pods or files by the UI. 

### Open file or pipe stdin

```sh
loggi tail file.log [more.log ...]   # tail files, opens browser
some-app | loggi stdin               # pipe stdin in
loggi docker my-container            # stream a docker container
```

### Other Commands

```sh
loggi web                            # open browser to a running server
loggi server status                  # show daemon state
loggi server stop                    # stop the daemon
loggi config print                   # print effective configuration
loggi init                           # scaffold a repo-level config

loggi profile list                   # list configured profiles
loggi profile show <name>
loggi profile use  <name>
loggi profile save <name>            # save current filter+columns+sources
loggi profile rm   <name>
loggi profile config print
```

`loggi tail`, `docker`, `stdin`, and `web` all auto-start the daemon when
needed. They connect over `/tmp/loggi-$UID.sock`.

## Web UI

The embedded SPA is the primary interface.

**Streaming list.** Virtualised log row list, prepend on live tail, infinite
scroll back through history. Per-source colour stripe on the left edge of
each row (hidden when only one source is active). Pin rows to keep them
anchored above the stream; diff two pinned rows side-by-side.

**Filter DSL** (Datadog-style):

```
level:error                                    # equality
service:batch_worker OR service:cron_worker    # OR
@msg_batch_conf.ConsumerQos:>=50               # nested JSON path (@ prefix)
*timeout*                                      # msg substring
level:>=info                                   # ordinal compare on level
-level:debug ts:[1777807314..1777807400]       # negation + range
/regex/                                        # regex on msg
```

Implicit AND between space-separated terms. Parens for grouping.

**Sidebar** (toggle with `⌘B` / `Ctrl+B`, the `panel-left` header button):

- **Filters** — chip-style clause builder; pinned filter chips persist
  across sessions and AND onto the working filter.
- **Facets** — vertical list of discovered field keys ranked by duplication
  score (high-duplication keys first). Expand a key to see its values with
  occurrence counts. Click a value to add `field:value` to the filter; click
  again to remove. Pin frequently-used keys or values to the top of their
  list. A per-key search box appears when a key has > 10 distinct values.
  Facets reset cleanly on filter/profile change and on source removal.
- **Sources** — per-source health badge, throughput, mute (`M`) and solo
  (`S`) toggles, remove. `+` button to add files, stdin, or docker
  containers from the UI.

All three sections are collapsible with persisted open/closed state. Drag
the sidebar's right edge to resize (persisted).

**Detail panel** (click a row, drag its left edge to resize). Shows the
parsed message, source, trace ID, and a JSON tree of fields. Each leaf has
`[+]` / `[•]` / `[-]` buttons to add, replace-all, or exclude that
`field:value` clause from the filter. Nested objects render with strict
2-space indentation. The trace row exposes the same three buttons for
`trace_id:<value>`.

**Header row** — drag column headers to reorder, drag right edge to resize
(including a "between message and next column" handle for the flex
message column). Columns menu lets you toggle built-ins and add custom
`@dotted.path` field columns. Timeline strip on top supports brush-to-zoom
into a time range.

**Highlight** with `⌘F` / `Ctrl+F` — substring or `/regex/` search inside
the visible buffer.

**Profiles** — save the current filter + visible columns + active sources
as a named profile; switching profiles reloads history and resets facets.
Stored in `~/.zz/loggi/profiles.toml` and merged with repo-local profiles.

## Architecture

- **Server.** Ingest goroutine per source → fan-in channel → ingester →
  store with columnar dictionary encoding + content-addressed blob slab →
  hub of subscribers with bounded channels and gap markers.
- **Transport.** Unix socket (length-prefixed JSON) for CLI clients;
  HTTP + WebSocket (same JSON envelope) for the embedded SPA.
- **Auto-start.** `client.Dial` connects to `/tmp/loggi-$UID.sock`,
  spawns `loggi server --daemon` if missing (with flock to avoid race),
  polls until reachable.
- **Storage.** Ring buffer (default 524288 entries) with per-field hot
  columns and promotion on threshold, global string interner with
  refcount, content-addressed blob slab for repeated nested objects
  (e.g. config dumps), `ts` as an F64 column. Eviction releases
  refcounts; identical 30 KB blobs across thousands of restarts are
  stored once.
- **Filter pipeline.** Parser → AST → compiler → evaluator. Compiled
  filters short-circuit on column dictionaries before touching row data.

## Configuration

Precedence (last wins):

1. `~/.zz/loggi/config.toml`
2. `<repo>/.loggi/config.toml` (or `<repo>/cmd/.config/loggi.toml` if `cmd/` exists)
3. `*.local.toml` sibling of (2)

See `loggi config print` for the live merged view.

## Layout

```
cmd/loggi/                main()
internal/app/             cobra subcommands + embedded SPA
internal/client/          dial + auto-start
internal/server/          listeners, hub, ingester, sessions
internal/store/           ring + columns + interner + blobs
internal/filter/          parser + AST + compiler + evaluator
internal/source/{file,stdin,docker}
internal/config/          TOML schema, precedence loader, repo detection
internal/frame/           length-prefix codec
internal/wire/            JSON envelope schema (server/client messages)
web/                      Svelte 5 + Vite + Tailwind
  src/App.svelte          main shell
  src/lib/FacetPanel.svelte
  src/lib/JsonTree.svelte
  src/lib/DetailPanel.svelte
  src/lib/FilterBuilder.svelte
  src/lib/SidebarSection.svelte
  src/lib/ColumnHeader.svelte
```

## Debug endpoints

Available when the server is started with `--debug` (or via
`./run server-debug`):

- `GET  /api/debug/store`    — current store stats (entries, columns, blobs)
- `POST /api/debug/filter`   — compile a filter expression and return its AST

## Author

Made by [iOliverNguyen](https://github.com/iOliverNguyen) with ❤️
