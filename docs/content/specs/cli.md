---
title: "CLI"
weight: 100
---


> Status: **draft (v0.1).**

Lodo is a binary you invoke. There is no daemon, no watch mode, and no internal
scheduler: cron (or a manual run) calling `lodo schedule` is the intended deployment.

## Commands

| Command | Side effects | Purpose |
| --- | --- | --- |
| `lodo` with no args on a TTY | none until an edit is saved; a saved edit rewrites one Logseq block | Interactive TUI: list Tasks, view raw blocks, edit in `$EDITOR` with per-Task validation (see [`tui.md`](./tui.md)) |
| `lodo check` | none alone; creates the Time Maps page only on confirmation | Validate the graph and state: TODOs missing `duration::`, unparseable or unknown properties, unknown `time-map::` slugs, `unknown-dependency` / `dependency-cycle`, infeasible Tasks (`blocked by …`, cycles, pin violations), stood-down Blocks, orphaned event IDs. |
| `lodo forecast` | none alone; creates the Time Maps page only on confirmation | Dry run: compute and print the Schedule that *would* be written, with shortfall reasons including dependency blocks. |
| `lodo schedule` | calendar + SQLite | The workhorse. Reconciles the calendar, computes, and creates/updates/removes Managed Events. |
| `lodo refresh` | SQLite only | Pull-only: read the calendar and fold the user's moves and deletes into local state, without recomputing. |
| `lodo explain <task-id\|uuid\|query>` | none alone; creates the Time Maps page only on confirmation | Read-only: explain why a Task was placed, moved, left short, or blocked. Uses the same reconcile + planner logic as `check`/`schedule` (calendar busy time and stored dispositions when available; degrades like `check` without calendar). Shows eligibility, effective/inherited properties, dependency gate, plan/window, Time Maps/tiers, rejection/comparator reasons (ADR 0010/0014), and final blocks or shortfall. |
| `lodo auth google` | credentials | One-time OAuth; stores the refresh token. |

### `lodo schedule` reconciles first

`schedule` always reads the calendar back **before** computing. It thus can never clobber
a move the user made between runs. `refresh` exists only for the narrower case:
"acknowledge my calendar edits without rebuilding the Schedule."

The alternative — having `schedule` trust local state and requiring the user to remember
`refresh` first — is precisely the clobber this design exists to prevent.

### Naming

`lodo schedule` reads as "produce the Schedule", which is consistent with the glossary's
use of *Schedule* as the noun, so the name is kept rather than replaced with
`run`/`sync`.

### Bare `lodo` opens the TUI

With zero args on an interactive terminal (stdin and stdout are TTYs), `lodo`
opens the TUI instead of printing help: the Task table, raw-block view, and
`$EDITOR` editing with a per-Task validation gate before any Logseq write
(see [`tui.md`](./tui.md)). `lodo --help`, any subcommand, `lodo --json`, or
zero args without a TTY keeps the existing path — the TUI never starts in
pipes, CI, or `--json` runs.

## Flags

| Flag | Applies to | Effect |
| --- | --- | --- |
| `--dry-run` | `schedule` | Same as `forecast`. |
| `--json` | all | Machine-readable result on stdout. Never launches the TUI. |
| `--log-format auto\|text\|json` | all | Diagnostic log format on stderr: `text` (default, `auto` is `text`), `json` is JSONL for CI/cron. Also `LODO_LOG_FORMAT`. |
| `--now RFC3339` | all | **Hidden testing aid.** Pin the scheduling clock (also `LODO_NOW`): the window, the busy mask, and store timestamps derive from one instant. Not for scheduled runs. |
| `--disable-colors` | all | Disable colors in human-readable output. Also off with `NO_COLOR`, `TERM=dumb`, or non-TTY. |
| `--no-launch-browser` | `auth google` | Headless machines: finish the grant on another device, paste the redirect back. |
| `--window-days N`, `--timezone TZ`, … | `schedule`, `forecast` | One-run overrides of config. Not persisted. |
| `--read-calendar ID` | `schedule`, `forecast`, `refresh` | Repeatable: replaces `calendar.read_calendars` for one run when given at least once. Not persisted. |
| `--logseq-token TOKEN` | `check`, `schedule`, `forecast`, `refresh`, `explain` | One-run Logseq token (overrides file and `LODO_LOGSEQ_TOKEN`). Not persisted. |
| `--logseq-graph-token NAME=TOKEN` | `check`, `schedule`, `forecast`, `refresh`, `explain` | Repeatable per-graph token (overrides file and `LODO_LOGSEQ_TOKEN_<GRAPH>`). Not persisted. |
| `--keep-token TOKEN` | `check`, `schedule`, `forecast`, `refresh`, `explain` | One-run unofficial Keep sync token (overrides file and `LODO_KEEP_TOKEN`). Not persisted. |
| `--keep-source-token NAME=TOKEN` | `check`, `schedule`, `forecast`, `refresh`, `explain` | Repeatable per-source Keep token (overrides file and `LODO_KEEP_TOKEN_<SOURCE>`). Not persisted. |
| `--google-client-id ID`, `--google-client-secret SECRET`, `--google-refresh-token TOKEN` | `check`, `schedule`, `forecast`, `refresh` | One-run Google credentials (override file and `LODO_GOOGLE_*`). Not persisted. |
| `--google-client-id ID`, `--google-client-secret SECRET` | `auth google` | Client pair for the flow (overrides file and env). |
| `--google-refresh-token TOKEN` | `auth google` | Store this grant directly without running the browser flow. |
| `--strict` | `check`, `schedule` | Treat reported problems as a non-zero exit. |
| `--verbose` | all | Show placement reasoning: `check`/`forecast`/`schedule` add per-task decision summaries (consideration order, tie-break, outcome); `explain` is always full detail and `--verbose` adds the whole consideration order. Full per-task detail lives in `lodo explain <task-id>`. |
| `--yes`, `-y` | `check`, `forecast`, `schedule`, `explain` | Answer yes to prompts: create a missing Time Maps page without asking. Not persisted. |

### Missing Time Maps page

`check`, `forecast`, and `schedule` need the Time Maps page (`logseq.time_maps_page`,
default `Time Maps`). When it is missing, Lodo offers to create it with the
recommended defaults — a `Deep Work` map marked `default:: true`, plus `Errands`
and `Weekends` windows (see [`time-maps.md`](./time-maps.md)) — which the user
then edits in the backend (Logseq graph or Keep note). Anything but an explicit `y`/`yes` declines (including
end-of-input, so a non-interactive run without `-y` never hangs), and the run
continues without the page, reporting `missing-time-maps-page` as before.
This confirmation is the bootstrap write; the TUI's explicit per-Task saves are
the second backend write path
(see [ADR 0017](../ADRs/0017-tui-edits-logseq-blocks-on-explicit-save.md) and
[`tui.md`](./tui.md));
`check` otherwise writes nowhere.

## Exit codes

- `0` — success. Reported problems (infeasible Tasks, missing durations) do not fail a run
  unless `--strict` is given.
- `1` — operational error (Logseq unreachable, calendar auth failed, bad config).
- `2` — validation failures under `--strict`.

## Output modes

Two audiences, two streams. Result payloads go to stdout; diagnostics go
to stderr, so `--json` stdout stays pipeable.

- Humans (default `ui.theme: concrete`): plain text on stdout — bold day headers, green
  placed/synced, yellow shortfalls/warnings, red orphans/errors, dim
  busy lines and event IDs when a color theme is selected — with tab-aligned block times and a
  `Summary:` footer per command. `ui.theme` set to a shipped scheme
  (e.g. `monokai`) or a custom theme file enables colors; colors stay off with `--disable-colors`,
  `NO_COLOR`, `TERM=dumb`, or a non-TTY. Errors print as
  `error: <what> — <fix>` (red under a color theme). See [themes](./themes.md).
- CI/cron (`--log-format=json` or `LODO_LOG_FORMAT=json`): stderr is
  JSONL, one object per line, with `timestamp` (RFC3339Nano UTC), `level`,
  `message`, plus `service: lodo`, `command`, `event`
  (`run.start`, `run.finish`, `calendar.warning`, `run.error`), and
  counts (`blocks`, `shortfalls`, `writes`, `orphaned`, `duration_ms`,
  `exit_code`). No tokens are logged. Pair with `--json` for a
  machine-readable result on stdout:

```sh
lodo --log-format=json --json schedule 1>result.json 2>run.log.jsonl
```

Datadog parses the JSONL natively via a JSON pipeline (`timestamp` as
the date, `level` as status); Loki parses it with `| json` and
`timestamp` as the detected time field. `--verbose` maps to `DEBUG`
records; without it the log is `INFO`/`WARN`/`ERROR` only.

### JSON result payloads

Every `--json` run writes exactly one JSON object to stdout. The object starts
with `schema_version`, whose value is per command:

| Command | `schema_version` | Top-level fields |
| --- | --- | --- |
| `check` | `1` | `graph_issues`, `shortfalls`, `suggestions`, `stood_down`, `orphaned`, `explanations` (with `--verbose`), `busy_sources` |
| `forecast`, `schedule --dry-run` | `1` | `blocks`, `shortfalls`, `suggestions`, `explanations` (with `--verbose`), `managed_left_out`, `busy_sources` |
| `schedule` | `1` | `blocks`, `shortfalls`, `suggestions`, `explanations` and `reconciled`/`dispositions` (with `--verbose`), `writes`, `orphaned`, `busy_sources` |
| `refresh` | `1` | `dispositions`, `adopted`, `captured`, `busy_sources`, `conflicts` |
| `explain` | `2` | `context`, `task`, the planner decision trace (`eligibility`, `ordering`, `effective_properties`, `dependency_gate`, `plan_window`, `time_maps`, `placement`, `final_blocks`, `outcome`), `dispositions`, `consideration_order_before`, `spillover`, `graph_issues`, `report`, `busy_sources` |
| `auth google` | `1` | `stored` |

The schema lives in `internal/wire`
([ADR 0023](../ADRs/0023-json-wire-schema-boundary.md)); `internal/cli/wire.go`
maps run outcomes onto it. A version changes by adding fields, never by
removing, renaming, or retyping one; readers ignore unknown fields. A shape
change bumps the version even when it is additive. A payload with no
`schema_version` is the unversioned pre-1 shape (version `0`).

`busy_sources` is present for every calendar-reading payload. `check` drops it
when no calendar was consulted; `explain` always carries it, and its
`context.calendar` says whether a calendar was consulted. Fields marked
`--verbose` appear only under that flag. When `explain` matches no Task or the
query is ambiguous, the payload carries `schema_version`, `error`, `query`, and
`matches` (each match has `id`, `text`, and `graph` when non-empty), and the
command exits `1`.

`explain` is the deep-dive payload: its per-task `eligibility`, `ordering`,
`dependency_gate`, `plan_window`, `time_maps`, `placement`, `final_blocks`, and
`outcome` are the planner's actual decision trace
(`internal/planner/trace.go`), so the explanation cannot drift from the
placement it describes. Those trace types are part of the versioned explain
schema. v2 adds `ordering` (including the actual `tie_break`/`tie_break_detail`
and consideration `position`/`total`), per-task `dispositions`,
`consideration_order_before`, `spillover`, and rejection breakdowns
(`rejected_external/pin/pinned/placed_earlier`,
`rejected_before_bound/outside_plan`) alongside the v1 fields.

The stderr JSONL records described above are diagnostics, not this result
schema.
