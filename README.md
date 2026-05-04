# loggi — local-first log viewer

A single-binary tool that runs a local server and serves a web UI for viewing
JSON-structured and ANSI-colored logs from files, stdin, or docker containers.
Multiple clients can attach to the same server; the server is auto-started on
demand.

## Build

```sh
# 1. Build the Svelte SPA (output goes to internal/app/spa/)
cd web && pnpm install && pnpm run build && cd ..

# 2. Build the Go binary (embeds the SPA)
go build -o loggi ./cmd/loggi
```

The binary is self-contained.

## Usage

```sh
loggi tail file.log              # tail a file, opens browser
loggi docker my-container        # stream a docker container
some-app | loggi stdin           # pipe stdin in
loggi web                        # ensure server, open browser
loggi server status              # show running server
loggi server stop                # stop the daemon
loggi config print               # print effective configuration
loggi init                       # scaffold a repo-level config
loggi profile list               # list configured profiles
```

## Filter DSL

Datadog-style:

```
level:error                                    # equality
service:batch_worker OR service:cron_worker_new
@msg_batch_conf.ConsumerQos:>=50               # nested JSON path
*timeout*                                      # msg substring
level:>=info                                   # ordinal compare
-level:debug ts:[1777807314..1777807400]       # negation + range
```

Implicit AND between space-separated terms. Parens for grouping.

## Architecture

- **Server**: ingest goroutine per source → fan-in channel → ingester → store
  with columnar dictionary encoding + content-addressed blob slab → hub of
  subscribers with bounded channels and gap markers.
- **Transport**: unix socket (length-prefixed JSON) for CLI clients; HTTP +
  WebSocket (same JSON envelope) for the embedded web SPA.
- **Auto-start**: `client.Dial` connects to `/tmp/loggi-$UID.sock`, spawns
  `loggi server --daemon` if missing (with flock to avoid race), polls until
  reachable.
- **Storage**: ring buffer (default 524288 entries), per-field hot columns
  with promotion on threshold, global string interner with refcount, content-
  addressed blob slab for repeated nested objects (e.g. config dumps), ts is
  a F64 column. Eviction releases refcounts; identical 30 KB blobs across
  thousands of restarts are stored once.

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
```

## Status

Phase 1 complete: server, web UI, file/stdin/docker sources, JSON parsing,
in-memory store with live compression, filter DSL, auto-start, profiles,
`loggi init`. TUI client (bubbletea) deferred to phase 2.
