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
agentscope serve --web dist    # the live view on http://127.0.0.1:8765/
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
| **cluster** | one per session (`<short id> <repo>`), and one "repositories" |
| **node** | the session's main agent (orange = active, grey-orange = idle), its sub-agents (colour = agent type), a node per repository held |
| **edge** | session to sub-agent (spawn, label = description), session to repository (lease, label = note; an expired lease is drawn **Idle**), sender to target (`tell`, label = text) |
| **table** | session, repository, task (live lease note, else title, else slug), last activity, #sub-agents |
| **detail** | the selection's facts; with nothing selected, the **anomalies** |
| **search** | dims the graph and narrows the table |

Click a node and its table row is selected, and the reverse. The graph shows the
newest 32 sessions, 3 sub-agents per session (then a "+N more" node) and 9
repositories (live leases first); the table and the detail pane list everything
and the status line says what the graph left out.

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
git clone https://github.com/go-agentsync/agentscope && cd agentscope
./build.sh                       # builds dist/: app.wasm, wasm_exec.js, index.html, ...
agentscope serve --web dist      # then open the printed URL
agentscope snapshot --json       # the exact message the browser receives
```

`snapshot` and `serve` take `--claude-dir`, `--localhost-root`, `--github-root`,
`--active` (a session is **active** when its transcript was written within this;
default 10m), `--horizon` (default 12h), `--tell-window` (24h) and `--max-agents`.
`serve` takes `--addr` (default `127.0.0.1:8765`), `--web` and `--interval` (2s).
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
```

CI runs actionlint, gofmt, vet, `-race` tests, the coverage gate (a Go program,
not awk), vet for darwin and windows, the wasm build through `build.sh`,
`govulncheck` judged by `vulngate`, `mvvmlint` and `bricolint` with a negative
control that proves the guard still bites.

## Known limits (v0.1.0)

Tracked as issues: the graph does not scroll and sizes every cluster like the
largest, so it is bounded as above; the wasm bundle is built with `build.sh`, not
embedded in the binary; the table's cells are cut to fit because `Table` does not
clip; Linux and Windows paths of `~/.claude` are the user's home on each, but only
macOS has been exercised.

## License

BSD-3-Clause. See [LICENSE](LICENSE).
