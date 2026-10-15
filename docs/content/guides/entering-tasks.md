---
title: "Entering Tasks"
weight: 10
---

How to enter a Task in Logseq (file-graph) so Lodo schedules it, including
`depends::` chains and nesting. Logseq is read-only for Lodo: you author intent
in the graph, Lodo projects Blocks onto the calendar.

## The 30-second Task

1. New block, type `TODO`, press `Enter`.
2. Add `duration:: 90m` (`2h`, `1h30m`). No duration = note, never scheduled.
3. Run `lodo check` — it names what is missing or unparseable, never guesses.

Copy-paste template (save as a Logseq template for `/Template`):

```
TODO {title}
duration:: 1h
time-map:: deep-work
priority:: B
plan-start:: 2026-10-20
plan-end:: 2026-10-24
depends::
```

Delete lines you do not need. Empty `depends::` means no predecessors. Dates are
day-granular; a specific time is a Pin (`scheduled:: <2026-10-20 Tue 09:30>`).

## Logseq shortcuts that speed this up (file-graph)

- `Enter` new block, `Shift+Enter` newline inside a block.
- `/` slash commands: `/Template` to insert your `lodo-task` template,
  `/Scheduled` + `/Deadline` + date picker for native dates.
- `((` or `/Block reference` + autocomplete to insert a `((uuid))` ref. Use this
  for `depends:: ((uuid)), ((uuid))` — never type UUIDs by hand.
- Right-click a block bullet → **Make Template**, name it `lodo-task`; then
  `/Template lodo-task` inserts the block above.
- `Tab` / `Shift+Tab` indent/outdent to nest. Indent under a `TODO` with
  `duration::` makes that parent a container (see below).
- TODO cycling on the marker (`TODO` → `DOING` → `DONE`) marks completion;
  `DONE`/`CANCELLED` satisfy dependents and suppress subtrees.

## Dependencies: `depends:: ((...))`

- Strict finish-to-start: no Block of B starts before all placed Blocks of A end.
- Multiple refs are AND: `depends:: ((A)), ((C))` waits for the last finisher.
- `DONE`/`CANCELLED` deps are satisfied immediately. Missing UUIDs, plain notes,
  or `TODO` without `duration::` are errors: `check` reports
  `unknown-dependency`, the dependent is blocked.
- Cycles (including self-refs) are hard stops: `dependency-cycle`, nothing in
  the cycle is placed.
- Partial deps block entirely; stood-down hours block until inputs change.
- Plan wins: a dep never pushes past `plan-end::` (reported `blocked by …`).
  Pin wins: a `scheduled::` stays, violation reported.

Fast flow: place cursor after `depends::`, type `((`, search the predecessor's
title, `Enter`; repeat with `, ((…))` for joins.

## Nesting: containers and `order:: sequential`

```
TODO Website by July 31  plan-end:: 2026-07-31  order:: sequential
  TODO Design landing page  duration:: 3h
  TODO Write copy  duration:: 2h
  TODO Notes without duration (checklist, never scheduled)
```

- A `TODO` + `duration::` with Task children is a **container**: never scheduled
  itself, own duration ignored (sum of leaves is the effort). Checklist lines omit
  `duration::`.
- `order:: sequential` chains descendant leaves in outline order; `parallel`
  (default) leaves siblings unordered. The flag cascades down until overridden;
  a page-level `order:: sequential` chains the whole page.
- `time-map::`, `priority::`, `plan-start/end::` cascade child > parents >
  page. `depends::` never inherits — cross-parent links stay explicit.
- `DONE` on a parent hides its subtree (unscheduled until reopened).

## Check feedback loop

- `lodo check`: `todo-missing-duration`, `bad-property-value`,
  `unknown-property`, `unknown-time-map`, plus `unknown-dependency` and
  `dependency-cycle`; shortfalls name `blocked by …`.
- `lodo forecast`: dry-run Blocks by day + `Shortfall:` with reasons
  (`blocked by incomplete dependencies`, `dependency cycle`, `unknown
  dependency`, `pin precedes dependency`).
- Fix in Logseq, re-run. No writes to the graph ever happen.
