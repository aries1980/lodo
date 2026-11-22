# Lodo — agent/developer map

Local-first CLI that reads schedulable Tasks from a Logseq graph, computes a Schedule, and
projects it to **one** Google Calendar. Intent is authored in Logseq or in the TUI
(ADR 0017); a TUI save writes the block into the graph, and scheduling reads it
from there. The CLI renders commands and output; `internal/planner` decides placements.

## Core pipeline

```mermaid
flowchart LR
  cli[cli] --> runPkg[run]
  runPkg --> logseq[logseq]
  runPkg --> resolvePkg[resolve]
  runPkg --> plannerPkg[planner]
  runPkg --> reconcilePkg[reconcile]
  runPkg --> calendarPkg[calendar]
  runPkg --> storePkg[store]
  resolvePkg --> parsePkg[parse]
  plannerPkg --> gridPkg[grid]
  parsePkg --> modelPkg[model]
  plannerPkg --> modelPkg
  gridPkg --> modelPkg
```

1. **Read** task-backend payloads over a neutral seam (`internal/graph`, ADR 0026):
   Logseq over its HTTP API (ADR 0002) via `internal/logseq`, Google Keep
   Workspace/Personal (`keep-workspace`/`keep-personal`) via
   `internal/keep/{workspace,personal}` sharing one mapping in
   `internal/keep`. Only two writes exist per backend: a missing Time Maps
   page is created on confirmation (ADR 0015), and the TUI saves one
   validated block (ADR 0017).
   `internal/run` walks pages and block trees. One task backend (a Logseq graph or
   a Keep note set) is a `run.Source`; several sources are read the same way
   (ADR 0021, ADR 0026).
2. **Resolve** (`internal/resolve`) turns those payloads into Tasks, Time Maps,
   dependencies, provenance, and validation issues. Resolution is deterministic and
   reports an issue when data is unparseable or ambiguous. Parsing lives in
   `internal/parse` and `internal/norm`.
3. **Plan** (`internal/planner`) is a pure function of resolved Tasks, Time Maps, a busy
   mask, dispositions, and scheduling config (ADR 0006). `internal/grid` is the slot
   grid it places onto (ADR 0008). `run.ForecastSources` (and its single-graph
   `run.Forecast` wrapper) is that plan with no store: the dry run behind `forecast`
   and `schedule --dry-run`.
4. **Reconcile and project** (`internal/reconcile`, `internal/calendar`, `internal/store`).
   `schedule` and `check` fold calendar edits into dispositions before planning
   (a move pins, a delete stands that Block down — ADR 0019).
   `internal/store` (SQLite) is the system of record for bindings, snapshots, and
   dispositions (ADR 0001). The calendar is a projection. Writes go to the calendar
   before the store commit (ADR 0007). `internal/gauth` is the Google OAuth loopback
   `internal/calendar` authenticates with.

`refresh` is the pull-only path: it reconciles calendar edits into the store and does
not resolve the graph or recompute the Schedule.

## Key packages

- `cmd/lodo/main.go`: process entrypoint; calls `cli.Run`.
- `internal/cli`: Cobra command tree, flags, config loading, command handlers, and
  human/JSON output. Keep wiring explicit. `prepare.go` holds the one `commandNeeds`
  table of what each command opens, and opens it. Read-only commands stay off the
  calendar and the store commit. New commands map their payload in `wire.go` and
  their text in `views.go`.
- `internal/run`: the command loop. `Forecast`/`ForecastSources` read, resolve, and
  plan. `Schedule` and `Check` reconcile, then plan. `Refresh` folds calendar edits
  without recomputing. `LoadGraph` fetches page trees with a bounded,
  order-preserving fan-out; `Schedule` applies calendar writes in
  delete/patch/insert phases with a bounded fan-out inside each phase.
  Forecast, check, and schedule share `Scheduled` (tasks, issues, schedule, busy);
  each outcome keeps only the fields that command adds.
- `internal/resolve`: graph payloads to Tasks, Time Maps, issues, and provenance.
- `internal/planner`: `Plan` produces a Schedule. The package comment names which file
  owns which stage. `search.go` emits Blocks; `comparator.go` is the ADR 0010 ordering.
- `internal/grid`: the Scheduling window as granularity slots.
- `internal/model`: domain types (Task, Block, Time Map, Disposition, Snapshot). Names
  stay aligned with the glossary. The types are plain data.
- `internal/parse`, `internal/norm`: parsing and small normalizations. Malformed input
  is an error; callers report issues.
- `internal/reconcile`: observed calendar events become pinned and stood-down
  dispositions (ADR 0019). Managed Event ids are derived here.
- `internal/calendar`: Google Calendar list/get/insert/patch/delete and busy masks.
  One calendar is written; the others are busy-only reads, fetched concurrently with
  audit rows and warnings kept in config order.
- `internal/gauth`: Google OAuth loopback for `lodo auth`.
- `internal/logseq`: Logseq HTTP API client, one implementation of the graph seam.
- `internal/graph`: the backend-neutral seam (`Page`, `Block`, `Client`, `Walk`).
- `internal/keep`: shared Keep mapping (note/item model, Time Maps outline engine);
  `internal/keep/workspace` (official REST, Workspace accounts) and
  `internal/keep/personal` (sync transport, personal accounts) implement the seam.
- `internal/store`: SQLite (`${XDG_STATE_HOME:-~/.local/state}/lodo/lodo.db` by
  default). Atomic `Commit`; opened per run (no daemon, ADR 0009). Schema is v0.1
  create-if-not-exists (no migration framework).
- `internal/config`: config and credentials (ADR 0005), plus per-run overrides.
- `internal/wire`: the `--json` result schema: payloads, shared row types, schema
  versions, and the `Payload` marker. The CLI maps run outcomes in
  `internal/cli/wire.go`; no other package declares a payload. See ADR 0023.
- `internal/jsonx`: shared JSON v2 encoding and decoding policy, preserving wire and
  store compatibility. `internal/set`: map-backed membership sets for shared plumbing.
  `internal/fanout`: bounded, order-preserving fan-out for independent indexed reads
  and calendar writes.

## Where to change X

- New CLI flag, command, or override: `internal/cli/*` (constructor, handler, tests).
  A new command also needs a `commandNeeds` row in `prepare.go`; that table is the
  single declaration of the config, graph, calendar, and store I/O a command touches.
  Map its payload in `wire.go` and its text in `views.go`.
- A JSON result field: the type and version in `internal/wire`, the mapping in
  `internal/cli/wire.go`, and `docs/content/specs/cli.md`, in one commit.
- Task, Time Map, dependency, or provenance resolution: `internal/resolve/*`, with
  `internal/parse` and `internal/model` when the shape changes.
- Placement rules, ordering, shortfalls, suggestions: `internal/planner/*`. Slot
  geometry: `internal/grid/*`.
- A config setting: field and `yaml` tag in `internal/config/config.go`; trimming,
  defaults, and errors live in that file's `validate`.
- Scheduling config (granularity, window, timezone): `internal/config`, then the
  `planner.Input` that `internal/run` builds.
- Persistence, bindings, snapshots, dispositions, run audit: `internal/store/*`
  (ADR 0007 write ordering and snapshot round-trip).
- Calendar read/write and busy masks: `internal/calendar/*`. OAuth: `internal/gauth/*`
  and the CLI auth flow.
- Read or write fan-out, worker count, or cancellation policy: `internal/fanout`
  (ADR 0024, ADR 0025).
- What forecast, schedule, check, and refresh do, and in which order: `internal/run/*`.

## Canonical docs (source of truth for intent)

- Glossary/specs: [`docs/content/specs/`](docs/content/specs/) (start at
  [`glossary.md`](docs/content/specs/glossary.md), [`overview.md`](docs/content/specs/overview.md),
  [`cli.md`](docs/content/specs/cli.md), [`scheduling.md`](docs/content/specs/scheduling.md),
  [`calendar.md`](docs/content/specs/calendar.md)).
- Decisions/ADRs: [`docs/content/ADRs/`](docs/content/ADRs/) (cite stable IDs like ADR 0001,
  ADR 0007, ADR 0009 in code when choices are non-obvious).
- Hugo site root: [`docs/`](docs/).

## Dev workflow

All tooling runs via podman or docker through [`scripts/dev.sh`](scripts/dev.sh) (no local Go/Hugo
required):

```sh
./scripts/dev.sh fmt
./scripts/dev.sh vet
./scripts/dev.sh lint
./scripts/dev.sh test
./scripts/dev.sh build
```

Before committing code changes, run the relevant checks (at minimum `fmt`, `vet`, and tests
for affected packages; run `lint` when practical). Keep files focused, preserve package
boundaries/invariants, and prefer explicit contracts over implicit global state.
