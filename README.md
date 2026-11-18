# agentscope

[![ci](https://github.com/go-agentsync/agentscope/actions/workflows/ci.yml/badge.svg)](https://github.com/go-agentsync/agentscope/actions/workflows/ci.yml)
[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD--3--Clause-blue.svg)](LICENSE)

A live graphical view of the Claude Code agent network on **this machine**: which
sessions and sub-agents exist, what task each has, which repository each holds,
and the messages between them. Built for the "about 25 sessions at once" case that
[`agentsync`](https://github.com/go-agentsync/agentsync) exists to coordinate, and
where `agentsync claims` can only list leases.

```
agentscope snapshot            # one snapshot in the terminal
agentscope serve               # the live view on http://127.0.0.1:8765/
```

The view is a graph of session clusters (each with its sub-agents) beside a
repositories cluster, a sessions table and a detail pane. It is not shown here as
a picture: a capture of a live view carries real session ids and lease notes, and
captures never go inside a repository.

## What it reads (read-only) and what it keeps

| Source | What is taken |
|---|---|
| `~/.claude/agentsync/locks/*.lease` | resource, owner session, note, acquired/expires |
| `~/.claude/agentsync/locks/<target>.messages` | `agentsync tell` sender, target, text (truncated), time |
| `~/.claude/agentsync/sessions/<uuid>.json` | **how many** entries and when the last was added; the entries themselves are memory-index text and are decoded into nothing |
| `~/.claude/projects/*/<session>.jsonl` | timestamp, cwd, git branch, slug, the session title, PR link, and the *names* of `SendMessage` recipients |
| `~/.claude/projects/*/<session>/subagents/agent-*.meta.json` | agent type, description, spawn depth |

**Privacy.** Only that metadata is extracted. A transcript line is decoded into a
struct that has no field for message content, so a prompt or a response is never
copied into memory, let alone onto the wire or the screen. A test feeds a prompt
and a response through every parser and fails if either shows up in the snapshot.
Nothing leaves the machine: the server binds a loopback address and refuses any
other (below).

Transcripts are untrusted input (they are written by a process that also holds
whatever a web page told it) and are hundreds of megabytes. They are read
**incrementally and bounded**: the first time a file is seen only its last 4 MiB
is read, afterwards only what was appended, from a remembered offset; a line over
1 MiB is skipped and counted; a half-written last line is left for the next pass;
a file that shrank is read again and reported.

## The picture

| | |
|---|---|
| **cluster** | one per session (`<short id> <repo>`), and one "other repositories" for repositories nobody holds |
| **node** | the session's main agent (orange = active, yellow = idle), its sub-agents (**labelled by their task**, the `description` in their meta; coloured by agent type), and **every repository it holds, inside its own cluster**, so who holds what is where it sits. Repository labels are `org/repo`; when that does not fit the ORG is shortened and the repo kept whole |
| **edge** | session to sub-agent (spawn), session to repository (lease), sender to target (`tell`) |
| **table** | session, **model**, repository, task (live lease note, else title, else slug), last activity, #sub-agents |
| **status line** | counts, and the sessions and sub-agents per model family ("models: Sonnet 29 \| Opus 23 \| Haiku 1"): where the token budget goes |
| **detail** | the selection's facts; with nothing selected, the **anomalies** |
| **search** | dims the graph and narrows the table |

**Focus.** With nothing selected only the edges of **active** sessions are drawn,
and dimmed (an expired lease's edge is not drawn at all); tells are always in full,
there are few and each is a message. Select a node and its edges are drawn in full
while the rest stay dimmed; select a session and its whole cluster is in focus.
Edges to **expired** leases are not drawn (the detail pane lists them in full); the
**expired leases** check button, bound to the ViewModel, draws them for a selection.
Select an edge to see it alone in full. Only edges change on a selection: no node
moves.

Repositories nobody holds are **folded by org**: one grey node per org with a count
(`+ go-authn (6)`). Enter, or a second click on it, expands it (`-`); Escape folds
every org again. The expansion is ViewModel state. A session with no live lease is
titled with the repository of its transcript's last cwd, marked `(cwd)` so it is not
mistaken for one it holds. GraphView has no hover events, so the full text of a
node (a sub-agent's whole description, a whole `org/repo`, an org's repositories) is
in the detail pane for the selected node.

Click a node and its table row is selected, and the reverse. Each cluster is sized
by its own members and the graph scrolls (wheel, scrollbar, arrow keys) when they
overflow, so every repository is reachable. Safety bounds on what is drawn: the
newest 64 sessions, 6 sub-agents per session (then a "+N more" node) and 200
repositories (live leases first); the table and the detail pane list everything
and the status line says what the graph left out.

### Look

The look is the toolkit's **Slate** theme (`toolkit.Slate(brand, dark)`), modelled on
the go-authn account page: a slate-50 page, white cards with a soft border and
shadow, slate-900 ink, and the brand `#0079a8` as the accent (its hover and
brand-ink variants are derived from the brand and match the page's). A header card
holds the Iconoir logo, the name, a **connection chip** and **model-count chips**;
the graph, the sessions table and the details each sit on their own card. Nothing is
drawn by hand in this repository: the cards are `Frame.Card`, the pills `Chip` with
`ChipBrand` / `ChipPlain`, the nodes and clusters `GraphView` with `CornerRadius`.
**Light and dark follow the system** (`prefers-color-scheme`, read by the wasm host
and followed when it changes); the graph's category palette is the Okabe-Ito order
(colour-vision safe) with its lightness tuned per theme, which keeps the order but
has not been run through a palette validator (none was at hand). The font is the
toolkit's embedded one; it cannot load another.

### Models

The model of a session and of each sub-agent is `message.model` of the **latest
assistant entry** of its transcript (a sub-agent's is in its own
`subagents/agent-<id>.jsonl`; only the tail is read, 4 MiB for a session and 256 KiB
for a sub-agent), so a `/model` change shows up. The table says "Opus 5.5"; the
detail pane has the raw id, and when more than one model was seen inside the
horizon, all of them, newest first. An id that does not match
`claude-(opus|sonnet|haiku|fable)-<major>[-<minor>]` is shown as `?` with the raw
id, and a transcript with no assistant entry as `? (none seen)`: the model is never
guessed. Claude Code's `<synthetic>` placeholder (an error or interruption) is
listed with the models seen but is never the latest. Only the id is decoded from an
entry: it has no field for its content. A sub-agent's model is in its detail (the
graph has no tooltip: GraphView has no hover); colouring nodes by model is not done.

### Anomalies are shown, not hidden

A lease key is whatever a session typed. Those that match a clone
(`~/Documents/VCS/GIT/localhost/<org>-<repo>[-topic]` or
`~/Documents/VCS/GIT/github.com/<org>/<repo>`) or an `org/repo` form are resolved
(`org/repo#topic`, `org/repo:topic` and `org/repo/topic` name the repository).
The rest -- a task note used as the key, a flag (`--note`), a bare name -- are kept
and listed under **ANOMALIES** as `unresolved key`, together with: a live lease
whose owner has no transcript, a tell from or to a session nobody knows, oversized
and unparsable transcript lines, unreadable files, a transcript that shrank.
Expired leases older than `--horizon` are not listed but are **counted**
(`expired leases beyond the horizon, not listed: N`), and their unresolved keys are
reported in one aggregate line.

## Architecture

```
 ~/.claude ──read──> collect.Collector ──snapshot──> wire.Hub ──gRPC/WebSocket──> wasm UI
                                                        │                          (ui.ViewModel
 agentscope serve: 127.0.0.1 only, Host-checked ────────┘                           + ui.Scene)
```

- `model` -- the snapshot (the schema is the Go type).
- `collect` -- the read-only collector (`agentscope snapshot`).
- `wire` -- the gRPC service `agentscope.v1.Scope` (`Snapshot` unary, `Watch`
  server-streaming) with a JSON codec both ends force, so there is no protoc step.
  The hub collects once per `--interval` however many browsers watch, and sends a
  snapshot only when something besides the clock changed, plus a keepalive.
- `serve` -- the loopback HTTP server: the UI and the gRPC endpoint on one origin.
- `ui` -- the **ViewModel** (all state as `go-widgets/mvvm` observables, no widget)
  and the **View** (`go-widgets/toolkit` widgets bound through `mvvmtk`). No
  hand-drawn UI. 100 % covered natively.
- `cmd/agentscope-web` -- the wasm canvas driver (the only file with a build tag).

**gRPC, in the browser.** The owner prefers gRPC over REST, so the wasm UI speaks
gRPC over [`grpc-transports/websocket`](https://github.com/grpc-transports/websocket):
its `js/wasm` client is the same code as the native one (`wire.Dial`), and the
tests run that same client over a real WebSocket against the real gRPC server. No
WebSocket JSON fallback was needed.

## Use

```sh
go install github.com/go-agentsync/agentscope/cmd/agentscope@latest
agentscope serve                 # then open the printed URL
agentscope snapshot --json       # the exact message the browser receives
```

The wasm UI is **built on first run**, not committed (it is 25 MB). `serve`:

1. finds the UI sources that belong to its own binary: after `go install` they are
   in the module cache at the version the binary was built from
   (`runtime/debug.ReadBuildInfo` plus `go env GOMODCACHE`); inside a checkout, the
   working tree;
2. compiles them with the local Go toolchain for `GOOS=js GOARCH=wasm` into
   `<user cache dir>/agentscope/<version>-<go version>/`, and takes `wasm_exec.js`
   from the `GOROOT` of that same toolchain. A new Go release or a changed source is
   a new entry, never an overwrite. Later runs start at once;
3. writes the entry into a temporary directory and renames it into place, so two
   first runs at once cannot corrupt it (the rename that wins is the entry, the
   other build is discarded and its run uses the winner's).

**What it needs:** the Go toolchain on `PATH`, and network access the first time
for the Go modules (the usual `go build` rules; `GOFLAGS=-mod=readonly`). With no
`go` it stops and names the way out: **`agentscope serve --web DIR`** serves a UI you
built elsewhere and needs no toolchain.

**What it will not build from:** a source directory, or any directory above it up to
the module cache, that is not owned by you or root, or that is writable by its group
or by everyone, because whoever can write there decides what the compiler runs.
Commands are run with an argument list, never through a shell. (Ownership checks
are unix; elsewhere the path is trusted.)

`snapshot` and `serve` take `--claude-dir`, `--localhost-root`, `--github-root`,
`--active` (a session is **active** when its transcript was written within this;
default 10m), `--horizon` (default 12h), `--tell-window` (24h) and `--max-agents`.
`serve` takes `--addr` (default `127.0.0.1:8765`), `--interval` (2s), `--web DIR`
(serve a directory instead of building the UI) and `--cache-dir` (where builds go).
`snapshot --stats --warm N` prints the time and allocation counts of one cold and
N incremental collections.

## Security model

What a page served here can reach is narrow, and each line below is a test:

- the listener refuses any address that is not a loopback IP literal;
- every request, the WebSocket upgrade included, must carry a `Host` naming the
  bound loopback address and port -- a DNS-rebinding page arrives with its own
  name and gets **421**;
- the WebSocket handshake also checks `Origin` against `Host` (the transport's
  same-origin default), so a foreign page cannot open the stream;
- GET and HEAD only; the only data is the read-only snapshot; no endpoint writes;
- `Content-Security-Policy: default-src 'none'; script-src 'self'
  'wasm-unsafe-eval'; ...`, `nosniff`, `no-referrer`, `no-store`;
- every parser is **fuzzed** (corpus in `testdata/fuzz/`, replayed by every
  `go test`) and bounded; `govulncheck` runs in CI.

## Performance

`agentscope snapshot --stats --warm 6` on the real tree of the machine that wrote
this (Apple M4 Max, ~300 leases and 59 transcripts in the last 12 h of a 15 GB
`~/.claude/projects`; **1-minute load 28-33, far above the ~8 limit for timings,
so only the allocation counts are exact**):

| | allocations | bytes allocated | time (under load) |
|---|---|---|---|
| cold snapshot (tail of every listed transcript) | 296 k | 95.6 MB | 185 ms |
| warm snapshot, before caching the resolver | 134.6 k | 8.6 MB | 20 ms |
| warm snapshot (each tick) | **20.8 k** | **2.6 MB** | 21-24 ms |

The profile of the first version put 73 % of a warm snapshot's allocations in the
lease-key resolver (called twice per lease, per tick); caching its answers until
the clone index is rebuilt took it from 134.6 k to 20.8 k allocations. The rest is
`readdir` and `stat`. `BenchmarkSnapshotRealTree` and `BenchmarkSnapshotFixture`
(a synthetic tree, comparable across machines) are in `collect/bench_test.go`.
There is no reference tool to compare against: `agentsync claims` reads leases
only. Timings on a quiet machine are an open item (issue filed).

## Development

```sh
go test -race ./...                                     # replays the fuzz corpus too
go test -coverprofile=coverage.out -coverpkg=./... ./... && \
  go run ./internal/covercheck -profile coverage.out -min total=90 -min ui=100
go test -fuzz '^FuzzParseLine$' -fuzztime 30s ./collect # also FuzzScan, FuzzResolve, ...
go vet -vettool="$(which mvvmlint)" ./...               # the MVVM gate
go vet -vettool="$(which bricolint)" ./...              # the no-hand-drawn-UI gate
go run ./internal/capture -out "$HOME/Library/Application Support/agentscope/captures/live.png"
                                                        # a screenshot, after the stream arrived
```

`internal/capture` drives a real Chrome over the DevTools protocol (headless
`--screenshot` fires at the load event, before the wasm has booted, and shoots an
empty window) and refuses to write inside a git work tree: a capture shows real
session ids and lease notes.

CI runs actionlint, gofmt, vet, `-race` tests, the coverage gate (a Go program,
not awk), vet for darwin and windows, a real first-run UI build from a clean cache,
`govulncheck` judged by `vulngate`, `mvvmlint` and `bricolint` with a negative
control that proves the guard still bites.

## Known limits

Timings were not taken on a quiet machine, only macOS has been exercised
([#2](https://github.com/go-agentsync/agentscope/issues/2)), and there are no org
landing or docs surfaces yet
([#3](https://github.com/go-agentsync/agentscope/issues/3)).

## License

BSD-3-Clause. See [LICENSE](LICENSE).
