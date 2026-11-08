# Lodo — agent/developer map

Local-first CLI that reads schedulable Tasks from a Logseq graph, computes a schedule, and
projects it to **one** Google Calendar. Logseq is the only interface for intent; the CLI is
the planner/writer.

## Core pipeline (mental model)

1. **Read** Logseq graph payloads (`internal/logseq`) — read-only.
2. **Resolve** graph data into planner inputs (`internal/resolve`): Tasks, Time Maps,
   dependencies/provenance, and validation issues. Resolve must be deterministic and must
   **not guess** when data is unparseable/ambiguous (report issues instead).
3. **Schedule/forecast** (`internal/model` types + CLI handlers): compute placements from
   resolved inputs, scheduling config, and calendar busy time. `forecast` is a dry run and
   stays read-only/store-free by design.
4. **Project/persist** (`internal/gcal` + `internal/store`): write Managed Events to the
   configured Google Calendar, then commit local state. `internal/store` (SQLite) is the
   **system of record** for bindings/snapshots/dispositions (ADR 0001); the calendar is a
   projection. Writes follow calendar-first commit ordering (ADR 0007).

## Key packages

- `cmd/lodo/main.go`: minimal entrypoint; calls `cli.Run`.
- `internal/cli`: Cobra command tree, flags/overrides, config loading, command handlers,
  output/logging, auth flows. Keep CLI wiring explicit; avoid hidden cross-cutting state.
- `internal/resolve`: graph -> Tasks/TimeMaps/issues/provenance. Deterministic ordering;
  consume parser-owned parsing; never guess.
- `internal/store`: SQLite state (`${XDG_STATE_HOME:-~/.local/state}/lodo/lodo.db` by
  default pathing in config/runtime). Atomic `Commit`; open/create per run (no daemon,
  ADR 0009). Schema is v0.1 create-if-not-exists (no migration framework).
- `internal/model`: domain types (Task/Snapshot/TimeMap/Disposition/etc.). Keep names aligned
  with the glossary.
- `internal/parse`, `internal/norm`: parsing/normalization primitives used by resolve/model.
- `internal/gcal`: Google Calendar integration and Managed Event projection.
- `internal/config`: config/credentials loading and per-run overrides.

## Where to change X

- New CLI flag/command/override: `internal/cli/*` (command constructor + handler + tests).
- Task/Time Map/dependency/provenance resolution: `internal/resolve/*` (+ `internal/parse` /
  `internal/model` as needed).
- Scheduling inputs/domain shape: `internal/model/*`, then `resolve`/CLI handlers.
- Persistence/bindings/snapshots/dispositions/run audit: `internal/store/*` (mind ADR 0007
  write ordering and snapshot/round-trip behavior).
- Calendar read/write behavior: `internal/gcal/*` and CLI schedule/refresh/auth flows.

## Canonical docs (source of truth for intent)

- Glossary/specs: [`docs/content/specs/`](docs/content/specs/) (start at
  [`glossary.md`](docs/content/specs/glossary.md), [`overview.md`](docs/content/specs/overview.md),
  [`cli.md`](docs/content/specs/cli.md), [`scheduling.md`](docs/content/specs/scheduling.md),
  [`calendar.md`](docs/content/specs/calendar.md)).
- Decisions/ADRs: [`docs/content/ADRs/`](docs/content/ADRs/) (cite stable IDs like ADR 0001,
  ADR 0007, ADR 0009 in code when choices are non-obvious).
- Hugo site root: [`docs/`](docs/).

## Dev workflow

All tooling runs via podman through [`scripts/dev.sh`](scripts/dev.sh) (no local Go/Hugo
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
