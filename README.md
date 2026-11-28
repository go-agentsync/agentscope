# agentscope

[![ci](https://github.com/go-agentsync/agentscope/actions/workflows/ci.yml/badge.svg)](https://github.com/go-agentsync/agentscope/actions/workflows/ci.yml)
[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD--3--Clause-blue.svg)](LICENSE)

A live graphical view of the Claude Code agent network on **this machine**: which
sessions and sub-agents exist, what task each has, which repository each holds,
and the messages between them. Built for the "about 25 sessions at once" case that
[`agentsync`](https://github.com/go-agentsync/agentsync) exists to coordinate, and
where `agentsync claims` can only list leases. Landing page:
<https://go-agentsync.github.io/>.

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
| `~/.claude/projects/**/*.jsonl` (the Quota tab) | per assistant message: timestamp, `message.id`, `message.model`, `usage.output_tokens`, `usage.cache_read_input_tokens`, summed per 10 minutes and model family |
| `aiquota status -o json` (the Quota tab) | the official usage windows: label, percent, reset instant; whether a window is available |
| `~/.claude/projects/**/*.jsonl` (the Guardrails tab) | per tool result a gate refused (an error result whose text starts `<Event>:<Tool> hook error:`): its time and the **gate its first words name**; the text itself is read to tell the gate and never kept |
| `recur --memory <dir> --json` (the Guardrails tab) | per lesson: name, recurrence sentences (truncated), the ledger's decision, text (truncated) and time |

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

### The whole surface, the panel and the tabs

The canvas is the browser's viewport: its size is the viewport times
`devicePixelRatio`, it is laid out again on every resize (the page has no scrollbars and no
maximum width), and the graph takes every pixel the header and the tab strip leave.
The layout is in logical pixels and the toolkit's metric scale (`SetMetricScale`) is set
to the device pixel ratio, so a 2x screen is drawn at 2x and not stretched. The
graph's clusters flow into as many columns as the width holds; a resize may lay the
graph out again, an edge update never does.

**Rendering is sober.** The canvas is repainted only when something it shows changed,
the user interacted, or the size changed: a snapshot that differs from the last one
only by its clock, its per-pass counters or the instant the usage view was built
repaints nothing (the status line has no clock for that reason), and the Quota
charts are not rebuilt while another tab is shown. While the tab is **hidden**
(`visibilitychange`) nothing is applied or drawn, the newest snapshot is kept and
shown once, in one repaint, when the tab is visible again. The backing store is capped
at **8 million device pixels** (the device pixel ratio is lowered to fit, and
re-fitted when the ratio changes, by a `resolution` media query), and one pixel buffer
and one `ImageData` serve every frame (they are made again only when the size
changes). `agentscopeStats()` in the page console returns the number of redraws and
the wasm heap.

The **sessions table and the details are a side panel docked beside the graph** (a
toolkit `Splitter`, 420 logical pixels by default), **closed by default**. Opening it
shrinks the graph to the width that is left (the graph lays itself out again, with
fewer cluster columns), closing it gives the width back; the panel never covers the
graph. The **Sessions (t)** button, the key `t` (when the search box is not focused)
and the **close button (xmark)** in the panel's top bar run the same ViewModel command
(`TogglePanel`); **Escape** closes it while the focus is in the panel. Selecting a
node does not open it (it would move the node you just clicked); its details are there
when you open the panel. The panel is not shown on the Quota tab.

**Resizing.** Drag the panel's left edge (the cursor becomes a resize cursor): the
graph shrinks as the panel grows. The panel stays between **320 px and 70 %** of the
width and the graph keeps at least 320 px. The width and the **column widths** of the
sessions table (drag a header separator; each column has its own minimum) are
ViewModel state, kept **per viewer in the browser's `localStorage`** (every access in
try/catch; a store that cannot be read leaves the defaults, and what it holds is
clamped).

**Splitting.** Drag the divider between **Sessions** and **Details** (a vertical resize
cursor; the keyboard works on it too) to give either more height. Each pane keeps at
least **120 px** and scrolls its own content; a window resize keeps the ratio and the
minimums. A **double-click** on the divider restores **55 % / 45 %**. The ratio is
ViewModel state, kept per viewer in `localStorage` like the width (a missing or corrupt
value is the default, a number is clamped to 10-90 %). When Sessions is at its minimum the
filter rows leave it so the table keeps the room.

**Filters** above the sessions table, all ViewModel state and combined with AND (and
with the search text): **state** (All / Active / Idle), **last activity** (any, < 10
min, < 1 h, < 6 h, < 24 h), **repository** (a session matches any repository it holds
or sits in), **model** (check boxes of the most used models present, "?" for unknown;
checked ones are OR-ed) and **Clear filters**. The status line says "N of M sessions
shown". The filters narrow the table only: the graph's dimming is a text match (the
search box), which a set of filters cannot express.

### Context pressure

The **Context** column of the sessions table, and the gauges at the top of the Details
pane (the selected session and each of its sub-agents), show how full a context is: the
`input_tokens + cache_read_input_tokens + cache_creation_input_tokens` of the **last API
call** of the transcript (entries of one `message.id` are one call; a synthetic entry
or one with no usage changes nothing), read incrementally and bounded like the rest,
against the compaction window: `autoCompactWindow` of `~/.claude/settings.json`, else
`CLAUDE_CODE_AUTO_COMPACT_WINDOW`, else 1,000,000. The gauge is the toolkit's
`Table.CellBar`, with the number beside it ("212k / 300k"), in five bands: below 40 %
dark green, 40-60 % light green, 60-75 % yellow, 75-90 % orange, 90 % and over red.
Graph nodes carry no tint: `GraphView` has no per-node colour hook.

### Quota

The second tab, **Quota**, has an **account selector** ("All (aggregated)" or one
account; ViewModel state), a span selector (**5 h**, **7 days**, **4 x 7 days**) and,
for the 4 x 7 days span, an output / cache-read selector.

**Accounts.** An account is named by the label aiquota gives it, its plan and a short
hash of its key (`Claude Code (team) #3d3c`); its e-mail and id are never sent to the
browser, drawn or recorded (only a SHA-256 prefix of the id is kept). The accounts
table has a **Plan** column (aiquota's reading; every recorded sample keeps the plan it
had) and a **Rename** button (select a row first): the display name is written by the
`serve` process, atomically with mode 0600, to `<user config dir>/agentscope/accounts.toml`,

```toml
"7567" = "Labo (team)"
```

(the key is the hash shown after `#`; you may edit the file by hand; a missing or
corrupt file means no alias at all). The alias replaces the label everywhere: the
selector, the table, the chart legends and the reset markers. With **`serve --show-org`**
(off by default) the Rename dialog starts with the organisation's name from
`~/.claude.json` as a *suggestion*; nothing is saved until you confirm, and without the
flag no organisation name is read, sent, drawn or logged. The renaming goes over the
same loopback WebSocket as the stream (same-origin only), as the one write the UI makes.

**Account colours.** In the "all accounts" quota view each account's lines and reset
markers are drawn in an automatic colour (by the account's position). The **Colour...**
button (select a row first) opens the toolkit's colour picker; **OK** gives the account
a colour of its own, **Reset** gives the automatic one back, **Cancel** (or Escape)
changes nothing. The choice is written by the `serve` process, atomically with mode
0600, to `<user config dir>/agentscope/account-colors.toml`, the same key as the aliases:

```toml
"7567" = "#12ab34"
```

A colour is exactly `#` and six hexadecimal digits (the picker's alpha is dropped, a
line is always opaque); anything else is refused by the server, and ignored in a file
edited by hand, which is read with the same 64 KiB bound as the aliases and at most 256
entries. A missing or corrupt file means the automatic colours; the colours survive a
restart.

*Official usage* comes from `aiquota status -o json` (run from `PATH`, fixed arguments, no
shell; aiquota already shares one reading between processes and serves stale data when
rate-limited, so agentscope reads it **at most every 5 minutes**), one reading per account
aiquota reports. aiquota keeps only its last reading, so agentscope **records the
history**: append-only JSON lines in `<user config dir>/agentscope/quota.jsonl`
(`--history-dir`), one line per account and reading, rotated to `quota.jsonl.1` past 4 MiB.
Each line carries the account key, label and plan. aiquota's JSON names an account only
by a label (every Claude Code account is "Claude Code"), so the key of a single-account
reading is the account **Claude Code is signed in to**, read from the `oauthAccount` of
`~/.claude.json` just before and after the call; if it changed in between, or cannot be
read, the reading is recorded with **no account**. Rows written before accounts were
recorded have no account either: they are shown as **"unknown account"**, never
assigned, and never counted in the capacity. A window the provider reports as not
available is **listed, never drawn as 0**. A line needs two recorded readings; until
then the page says *recording since <date>*.

*Local consumption* is derived from the transcripts: tokens per model family (Opus, Sonnet,
Haiku, Fable, stacked), **output and cache-read apart** (they differ by three orders of
magnitude, so they are two charts). One API message is split over several transcript
entries that share a `message.id`; within a file the entries are **counted once** (the larger
counts win). The transcripts are read **incrementally**: the scanner remembers each file's
offset and keeps only per-10-minute sums per account, cached in
`<user cache dir>/agentscope/usage.json`, so a restart reads what was appended and not
gigabytes (measured 2026-11-20, load 4: a cold scan of 12.4 GiB / 1470 transcripts takes
26 s; a restart with the cache 46 ms); a transcript untouched for 35 days is not opened.

**How a call is attributed to an account.** The assistant entries of a transcript carry
no account. Claude Code writes a `session-owner` entry (account id, organisation id,
session id; no timestamp) when a session starts or resumes, and the scanner attributes
every assistant entry to the **latest session-owner entry before it in the same file**.
An entry before the first one, or in a file that has none (older Claude Code versions),
**falls back to the stamps timeline** (below): the latest stamp at or before the call.
With no stamp that old either it is **unattributed**: drawn as its own grey layer in the
aggregated charts, counted in the notes, and **never given to a guessed account**. Order:
the session-owner entry, else the stamps, else unattributed. Gaps: a session whose
account changed without Claude Code writing a new session-owner entry is attributed to
the account at its start or last resume; and the transcripts written before Claude Code
recorded the owner, and before the hook below was installed, are unattributed.
`~/.claude.json` holds only the account signed in *now*, and the aiquota stamps carry no
account id, so neither can date an account switch: that is what the hook is for.

**`agentscope stamp`** is a hook verb for Claude Code's `SessionStart` and
`UserPromptSubmit` events. It reads the hook payload on stdin and the signed-in account
from `~/.claude.json`, and appends `{at, session_id, account_hash, label, plan}` to
`<user config dir>/agentscope/accounts.jsonl` **only when the account changed** since the
last line (locked, so ~25 sessions can share the file; rotated to `accounts.jsonl.1` past
256 KiB). It prints nothing on stdout (a hook's stdout reaches the model), makes no
network call, and exits 0 whatever happens (`--verbose` says why on stderr). Wire it in
`~/.claude/settings.json` yourself (agentscope never edits it):

```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [ { "type": "command", "command": "agentscope stamp" } ] }
    ],
    "UserPromptSubmit": [
      { "hooks": [ { "type": "command", "command": "agentscope stamp" } ] }
    ]
  }
}
```

(use the full path of the binary if `agentscope` is not on the hook's `PATH`).

**Aggregated view.** Each account's 5 h and weekly percentages side by side in a table
with the account-weeks of capacity left, Σ(100 − weekly %)/100, each account's next
weekly reset, and which account is active; accounts with a reading whose reset has
passed, and the unknown account, are listed and not counted. The 5 h and 7 day charts draw
one line per account with its own reset marker; the tokens are summed over accounts
(plus the unattributed layer).

**4 x 7 days.** For one account, four weekly windows aligned on **that account's weekly
reset instant aiquota reports** (not calendar weeks; earlier resets are found by stepping
back 7 days; if aiquota cannot be read the last recorded reset is used and the page says
so): one stacked bar per week, output or cache-read (selectable), four lines of cumulative
consumption over days 0 to 7 of each window on one axis, and the final official weekly
percentage of each week where it was recorded ("not recorded" where it was not). The
current week is marked in progress. In the aggregated view the accounts have different
reset anchors, so it is aligned on **calendar days** (28 days of tokens per day) and each
account's resets, one week apart, are its own marker series on the weekly-percentage chart.

### Guardrails

The third tab, **Guardrails**, answers one question: do the fleet's gates work?

- **Chips.** Untriaged lessons; **leaking gates** (a landed gate with a recurrence dated
  *after the day it landed*; a recurrence on the day itself may be the one that prompted
  the gate, so it is not counted); **silent gates**; refusals this week against last.
- **Recurrences per week** (12 weeks, Monday to Sunday, local time): the dated
  recurrences `recur` found, with those whose year `recur` had to *infer* drawn as their
  own line, so a viewer can tell stated from inferred. Dashed vertical lines mark every
  moment a gate was written into the ledger. Undated recurrences cannot be placed: they are
  counted in the lessons table and named in the notes line.
- **Gate refusals per week, stacked by gate**, from the transcripts, sub-agents included.
  A refusal is attributed to its gate by what its text says (guard-bash, replystamp,
  agentsync gate, recur, mutsweep, modtidy, modelgate, ctxgate, ghbyhand, prbody,
  paritycheck, quotaclaim); the table was built from a week of real refusals, and one no
  rule recognises is counted as **other gate**, never dropped. Only an error result whose
  text *starts* like a refusal counts: a file or a test log that quotes one does not.
- **Lessons table**, one row per lesson, sortable by any column (click a header) and
  filtered by decision: name, recurrences, last recurrence, decision, the gate's text
  (truncated), and **recurrences after the gate** counted from the ledger's `writtenAt`,
  red when above 0: *a gate that leaks is the headline*. Selecting a row lists its
  recurrence sentences, year-inferred ones marked.
- **Silent gate** (amber flag): a landed gate at least 4 weeks old, **whose ledger text
  names a hook the transcripts show** (`replystamp`, `mutsweep -hook`, ...), with no
  refusal from any of them in the last 4 weeks and no recurrence since it landed: either
  obsolete or no longer running. A gate that is a test or a CI step has no refusals to
  count and is never flagged.

Nothing is ever shown as 0 for what could not be read: if `recur` is missing, fails or
prints something that is not its JSON, the chips and the notes say **not measured** and
give the reason; the transcripts' half is measured on its own. `recur` is run at most
every 10 minutes with a 30 second timeout, and its answer is kept between runs.
`--memory-dir DIR` names the memory directory it reads; by default it is the one under
`<claude dir>/projects/*/memory` whose `.recur-ledger.md` was written last.

### Look

The look is the toolkit's **Slate** theme (`toolkit.Slate(brand, dark)`), modelled on
the go-authn account page: a slate-50 page, white cards with a soft border and
shadow, slate-900 ink, and the brand `#0079a8` as the accent (its hover and
brand-ink variants are derived from the brand and match the page's). A header card
holds the Iconoir logo, the name, a **connection chip** and **model-count chips**;
the graph, the sessions table, the details and each chart sit on their own card. Nothing is
drawn by hand in this repository: the cards are `Frame.Card`, the pills `Chip` with
`ChipBrand` / `ChipPlain`, the nodes and clusters `GraphView` with `CornerRadius`.
**The theme is a choice**: the header's **Theme** selector offers System (the default:
`prefers-color-scheme`, read by the wasm host and followed when it changes), Light and
Dark, through the toolkit's `ThemeManager`. The **Language** selector offers Auto
(the browser's `navigator.languages`, negotiated against the catalog), English and
French; the interface's words live in `ui/i18n.go` (a `toolkit.Catalog`, kept
complete by `i18ntest.Check`) and every selector, column title, button, dialog and
chart title follows the language when it changes. Both choices are the owner's: the
server keeps them in `settings.toml`, and a
view that connects later opens in them. Texts built from the data (details, notes,
anomaly messages) are still English; the graph's category palette is the Okabe-Ito order
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
(serve a directory instead of building the UI), `--cache-dir` (where builds go),
`--memory-dir` (see Guardrails) and `--show-org` (see Accounts; off by default).
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
go run ./internal/capture --out "$HOME/Library/Application Support/agentscope/captures/live.png"
                                                        # a screenshot, after the stream arrived
go run ./cmd/agentscope capture --scenario colors --out demo.svg   # see below
```

### `agentscope capture`: an animated, scalable demo

```sh
agentscope capture --list                              # the scenarios
agentscope capture --scenario theme-lang --out demo.svg
```

<img src="https://go-agentsync.github.io/img/agentscope-demo.svg" width="640" alt="agentscope recorded by agentscope capture: Light to Dark, English to French">

Renders a synthetic scenario of the UI, through the same view-model and the same
scene as the live window, into one animated SVG: text is drawn as glyph outlines
(it scales without blur), the steps loop with CSS keyframes, and a `<title>`
carries each string for screen readers. Scenarios: `overview` (tabs, quota,
guardrails), `colors` (account colours: the Colour dialog, two picks, the chart
repainted) and `theme-lang` (Light to Dark, then English to French). `--size
WxH` sets the window (320 to 4096 a side, default 1280x720).

**WebM, for the platforms that take video only.** `--format webm` (or an `--out`
ending in `.webm`; a format that contradicts the extension is refused) rasterises
the very frames of the animated SVG, each shown for its step's hold, and encodes
them as VP8 in a WebM with [avkit](https://github.com/go-avkit/avkit)'s pure-Go
`vp8enc` (the bitstream is [RFC 6386](https://www.rfc-editor.org/rfc/rfc6386);
the container follows the
[WebM guidelines](https://www.webmproject.org/docs/container/)): same scenario,
same view-model, no second rendering path.

```sh
agentscope capture --scenario overview --format webm --out overview.webm
```

The encoder runs at its slow preset (`CPUUsed` 4, screen-content mode), which
keeps UI text sharp, at a constant quantizer: `--quality 1` (best) to `63`
(worst), default 10. A 1280x720 capture is 230 to 410 KB and takes 0.5 to 0.8 s.
The size must be a multiple of 16 on both sides (1280x720 is): the encoder reads
whole macroblocks, and a size that is not is refused, with a message, until the
encoder pads its planes. The bytes are the same on every run (a test encodes
one scenario three times and compares them; the muxer writes no clock). A test
decodes every scenario back, at 640x384 and at the default 1280x720, and asserts at
least 40 dB PSNR against the SVG frames; another checks the file against the
guidelines (DocType `webm`, Codec ID `V_VP8`, Cues before the
first Cluster, a key frame first, carrying the right size); the leak scan reads
the WebM's bytes as well.

Nothing real is read: accounts, repositories and hosts are made up
(`acme-demo/*`, "Acme Lab"), the clock is fixed, and the output is byte-for-byte
the same on every run. The default `--out` is under the user config directory
(`<config>/agentscope/captures/<scenario>.svg`), and a path inside a git work tree
is refused. A leak test builds a deny-list from this machine's own agentscope and
aiquota configuration, `~/.claude.json` and `git config user.email`, adds a
pattern for any e-mail and any `github.com/<org>`, scans every drawn string, every
title and attribute of every scenario, and proves it by injecting a real-looking
value that it must catch. A capture is 2.5 to 3.6 MB raw (about 700 KB gzipped, which
Pages and most hosts apply): glyph outlines (about 60 %) and one-pixel rectangles
from the chart and graph (about 35 %) are the price of text that scales; the five
embedded PNGs are 4 KB.

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
([#2](https://github.com/go-agentsync/agentscope/issues/2)). The organisation
landing page is <https://go-agentsync.github.io/>; there is no docs site page yet.

## License

BSD-3-Clause. See [LICENSE](LICENSE).
