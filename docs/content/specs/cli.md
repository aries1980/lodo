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
| `lodo check` | none alone; creates the Time Maps page only on confirmation | Validate the graph and state: TODOs missing `duration::`, unparseable or unknown properties, unknown `time-map::` slugs, `unknown-dependency` / `dependency-cycle`, infeasible Tasks (`blocked by …`, cycles, pin violations), stood-down Blocks, orphaned event IDs. |
| `lodo forecast` | none alone; creates the Time Maps page only on confirmation | Dry run: compute and print the Schedule that *would* be written, with shortfall reasons including dependency blocks. |
| `lodo schedule` | calendar + SQLite | The workhorse. Reconciles the calendar, computes, and creates/updates/removes Managed Events. |
| `lodo refresh` | SQLite only | Pull-only: read the calendar and fold the user's moves and deletes into local state, without recomputing. |
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

## Flags

| Flag | Applies to | Effect |
| --- | --- | --- |
| `--dry-run` | `schedule` | Same as `forecast`. |
| `--json` | all | Machine-readable output. |
| `--no-launch-browser` | `auth google` | Headless machines: finish the grant on another device, paste the redirect back. |
| `--window-days N`, `--timezone TZ`, … | `schedule`, `forecast` | One-run overrides of config. Not persisted. |
| `--read-calendar ID` | `schedule`, `forecast`, `refresh` | Repeatable: replaces `calendar.read_calendars` for one run when given at least once. Not persisted. |
| `--logseq-token TOKEN` | `check`, `schedule`, `forecast`, `refresh` | One-run Logseq token (overrides file and `LODO_LOGSEQ_TOKEN`). Not persisted. |
| `--logseq-graph-token NAME=TOKEN` | `check`, `schedule`, `forecast`, `refresh` | Repeatable per-graph token (overrides file and `LODO_LOGSEQ_TOKEN_<GRAPH>`). Not persisted. |
| `--google-client-id ID`, `--google-client-secret SECRET`, `--google-refresh-token TOKEN` | `check`, `schedule`, `forecast`, `refresh` | One-run Google credentials (override file and `LODO_GOOGLE_*`). Not persisted. |
| `--google-client-id ID`, `--google-client-secret SECRET` | `auth google` | Client pair for the flow (overrides file and env). |
| `--google-refresh-token TOKEN` | `auth google` | Store this grant directly without running the browser flow. |
| `--strict` | `check`, `schedule` | Treat reported problems as a non-zero exit. |
| `--verbose` | all | Explain placement decisions. |
| `--yes`, `-y` | `check`, `forecast`, `schedule` | Answer yes to prompts: create a missing Time Maps page without asking. Not persisted. |

### Missing Time Maps page

`check`, `forecast`, and `schedule` need the Time Maps page (`logseq.time_maps_page`,
default `Time Maps`). When it is missing, Lodo offers to create it with the
recommended defaults — a `Deep Work` map marked `default:: true`, plus `Errands`
and `Weekends` windows (see [`time-maps.md`](./time-maps.md)) — which the user
then edits in Logseq. Anything but an explicit `y`/`yes` declines (including
end-of-input, so a non-interactive run without `-y` never hangs), and the run
continues without the page, reporting `missing-time-maps-page` as before.
This confirmation is the one graph write Lodo ever makes
(see [ADR 0015](../ADRs/0015-bootstrap-missing-time-maps-on-confirmation.md));
`check` otherwise writes nowhere.

## Exit codes

- `0` — success. Reported problems (infeasible Tasks, missing durations) do not fail a run
  unless `--strict` is given.
- `1` — operational error (Logseq unreachable, calendar auth failed, bad config).
- `2` — validation failures under `--strict`.
