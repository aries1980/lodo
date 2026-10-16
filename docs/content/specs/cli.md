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
| `lodo check` | none | Validate the graph and state: TODOs missing `duration::`, unparseable or unknown properties, unknown `time-map::` slugs, `unknown-dependency` / `dependency-cycle`, infeasible Tasks (`blocked by …`, cycles, pin violations), stood-down Blocks, orphaned event IDs. |
| `lodo forecast` | none | Dry run: compute and print the Schedule that *would* be written, with shortfall reasons including dependency blocks. |
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
| `--strict` | `check`, `schedule` | Treat reported problems as a non-zero exit. |
| `--verbose` | all | Explain placement decisions. |

## Exit codes

- `0` — success. Reported problems (infeasible Tasks, missing durations) do not fail a run
  unless `--strict` is given.
- `1` — operational error (Logseq unreachable, calendar auth failed, bad config).
- `2` — validation failures under `--strict`.
