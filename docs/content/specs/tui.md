---
title: "TUI"
weight: 110
---


> Status: **draft (v0.1).** Definitions live in [`glossary.md`](./glossary.md).

Bare `lodo` opens an interactive terminal UI over the same resolved Tasks the
CLI commands use. The TUI is an interface for intent, beside Logseq: a Task
list, a raw-block viewer, and a validated editor. A save writes the block into
the graph ([ADR 0017](../ADRs/0017-tui-edits-logseq-blocks-on-explicit-save.md)).
It does not plan, and it does not touch the calendar or the store.

## Entry

| Invocation | Behaviour |
| --- | --- |
| `lodo` with zero args on an interactive terminal (stdin and stdout are TTYs) | TUI |
| `lodo --help`, any subcommand, `lodo --json`, or zero args without a TTY | existing CLI path (help or the subcommand); the TUI never starts |

`--config` still selects the config file. `--json` never launches the TUI:
machine-readable output stays pipeable.

## List

The TUI lists resolved Tasks in a table: Task text, graph, duration,
Time Maps, priority, deadline. The rows are the `forecast`/`check` Task set
in outline order; graph issues are counted, not listed (see `lodo check`).

## View (`enter`)

`enter` shows the selected Logseq block's full raw `Content` in a scrollable
viewport with syntax highlighting layered on, never transformed: `**bold**`,
`` `code` ``, `[[links]]`, `((uuid))` block refs and `key:: value` property
lines stay verbatim, only colorized. A footer shows the resolved effective
properties (duration, Time Maps, priority, deadline) and marks inherited
values with their source. Viewing is read-only: no calendar, store, or graph
I/O.

## Edit (`e`)

`e` edits the full Logseq block in `$VISUAL` / `$EDITOR` (fallback `vi`):

1. The full block `Content` is written to a temp file with `# lodo-tui:`
   hint lines, which are stripped on return and never saved.
2. The TUI suspends, the editor runs, and the edited file is read back.
3. The edit is validated (see below) before any Logseq write.
4. A clean edit saves with `logseq.Editor.update_block` and the list
   re-resolves with the cursor kept on the same Task.
5. A blocked edit stays in the TUI with the issues listed: `e` re-edits,
   `t` fixes `time-map::` from a dropdown of resolved slugs
   (`(default)` clears the line), `esc` discards.

## Validation gate

Validation overlays the edited `Content` onto an in-memory copy of its graph
and reruns `resolve` — the same logic `check` uses. The save is blocked on
any issue referencing that Task or block, except `unknown-property` (left
alone by design). Demoting a Task (removing `duration::`, unmarking
`TODO`/`DOING`) does not validate: v0.1 edits keep the block a Task.

`time-map::` slugs must resolve against the graph's Time Maps; `duration::`,
`priority::`, `order::`, `depends::`, `deadline::`, `plan-start::`,
`plan-end::`, and `scheduled::` parse with the same rules as
[`tasks.md`](./tasks.md). Unknown `time-map::` offers the dropdown fix
without forcing a re-edit.

## What the TUI never does

- No planning, no Schedule, no suggestions.
- No calendar reads or writes; no store reads or writes.
- No Time Maps bootstrap prompt: a missing Time Maps page is reported like
  `check` reports it. Creating the page stays in `check`/`forecast`/`schedule`.
- No multi-line `time-map::` editing in the dropdown: the fix replaces the
  line with one slug (or clears it).
