---
title: "Entering Tasks"
weight: 10
---

How to enter a Task in Logseq (file-graph) so Lodo schedules it, including
`depends::` chains and nesting. Logseq holds intent and Lodo only reads it —
you author in the graph, Lodo projects Blocks onto the calendar — with one
exception: if the Time Maps page is missing, Lodo offers to create it with
recommended defaults on your confirmation (see
[Missing Time Maps page](../specs/cli.md#missing-time-maps-page)).

Spec references: [`tasks.md`](../specs/tasks.md) (what a Task is),
[`scheduling.md`](../specs/scheduling.md) (ordering and placement),
[`calendar.md`](../specs/calendar.md) (busy time).

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

## Page front matter vs block properties

Logseq file-graph has no YAML `---` front matter. There are two places
properties live in a `pages/*.md` file:

- **Page properties** — bare `key:: value` lines at the very top of the file,
  before the first `- ` bullet. They are defaults for every Task on the page.
- **Block properties** — indented `key:: value` lines under a `- TODO` bullet.
  They describe (or override for) that one Task.

Only five keys inherit (child block > parent blocks, nearest first > page >
default): `time-map::`, `priority::`, `plan-start::`, `plan-end::`, `order::`.
Everything else (`duration::`, `deadline::`, `scheduled::`, `depends::`) is
per-block and never inherits.

### Project pattern: one Time Map per client

Put `time-map::` on the project parent block. Every child TODO inherits it
without repeating the property, and overload spills tier-first into the
default map instead of shortfalling.

```markdown
- Company A
  time-map:: company-a
  - TODO Review access request
    duration:: 2h
  - TODO Update supplier list
    duration:: 1h
```

What Lodo sees: both leaves prefer `company-a` hours; when `company-a` is
full the remainder spills into the default map, green-default before
yellow-primary.

Raw file example (`pages/thesis.md` on disk):

```markdown
time-map:: deep-work
priority:: B
plan-end:: 2026-10-24

- TODO Write literature review
  duration:: 3h
- TODO Draft methodology section
  duration:: 2h
  priority:: A
  plan-end:: 2026-10-21
- TODO Buy printer paper
  duration:: 30m
  time-map:: errands
```

What Lodo sees:

- Both thesis Tasks inherit `time-map:: deep-work` from the page; the errand
  overrides it to `errands`.
- Both inherit `priority:: B`; the methodology draft overrides to `A` and is
  considered first among flexible Tasks.
- Both inherit `plan-end:: 2026-10-24`; the methodology draft tightens it to
  `2026-10-21` and becomes time-bound-urgent.
- `duration::` is never inherited: each block carries its own.

In the Logseq UI the same page looks like three top-level bullets with the
properties hidden under each block; the page properties sit in the page's
property drawer. What you type and what lands in the file are the same keys.

## Dependencies: `depends:: ((...))`

Strict finish-to-start: no Block of B starts before all placed Blocks of A end.

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

### Worked example: linear chain on one page

Raw file (`pages/launch.md` on disk — UUIDs filled in by `((` autocomplete,
never typed):

```markdown
- TODO Draft announcement
  duration:: 2h
  time-map:: deep-work
  id:: 67f3a1b2-c4d5-4e6f-a7b8-901234567890
- TODO Review announcement
  duration:: 1h
  time-map:: deep-work
  depends:: ((67f3a1b2-c4d5-4e6f-a7b8-901234567890))
  id:: 08b4c2d3-1111-4e6f-a7b8-222233334444
- TODO Publish announcement
  duration:: 30m
  depends:: ((08b4c2d3-1111-4e6f-a7b8-222233334444))
  plan-end:: 2026-10-24
```

What Lodo sees:

1. `Draft` has no deps → placed first, front-loaded in `deep-work` green hours.
2. `Review` waits for every placed Block of `Draft` to end, even though both
   are flexible. Topological order overrides the priority/outline tie-break.
3. `Publish` waits for `Review`. If `Draft` only partly fits (say 1h of 2h
   available before `plan-end:: 2026-10-24`), `Review` and `Publish` are left
   wholly unscheduled with `blocked by Draft` — dependents wait for the *full*
   dependency, never a partial.

### Worked example: join across pages

A `depends::` can point at any page — outline position does not matter:

```markdown
# pages/thesis.md
- TODO Get supervisor sign-off
  duration:: 30m
  depends:: ((aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee)), ((ffffffff-1111-2222-3333-444444444444))
```

```markdown
# pages/lab.md
- TODO Finish experiment run
  duration:: 4h
  id:: aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee
- TODO Calibrate sensor
  duration:: 1h
  id:: ffffffff-1111-2222-3333-444444444444
```

`Sign-off` starts after *both* lab Tasks finish (AND). Marking one lab Task
`DONE` satisfies that leg immediately; the other leg still gates.

## Nesting: what indented blocks mean

Outline hierarchy is scheduling intent, SkedPal-style. Indent with `Tab`; what
lands in the file is a nested `- ` bullet:

| Shape in Logseq | File shape | Scheduling meaning |
| --- | --- | --- |
| `TODO` + `duration::` with Task children | parent bullet with nested `- TODO` + `duration::` lines | **Container**: never scheduled itself, own `duration::` ignored; sum of leaves is the effort |
| `TODO` without `duration::` under anything | nested `- TODO` with no `duration::` | **Checklist**: a note, never scheduled, never makes the parent a container |
| `order:: sequential` on a parent | `order:: sequential` indented under the parent bullet | Chain descendant leaf Tasks in outline order (each waits for the previous) |
| `order:: parallel` (default) or absent | no line, or `order:: parallel` | Siblings unordered: priority, then outline order |
| `DONE`/`CANCELLED` parent | `- DONE ...` with nested bullets | Suppresses whole subtree: children unscheduled until it reopens |
| Plain bullet / `NOTE` parent | `- ...` with nested `- TODO` lines | Ignored for ordering; children scheduled as if top-level (but still inherit properties) |

Inheritance reminder: `time-map::`, `priority::`, `plan-start/end::`,
`order::` cascade child > parent blocks (nearest first) > page > default.
`duration::`, `deadline::`, `scheduled::`, `depends::` never inherit — a
cross-parent link stays an explicit `depends::`.

### Worked example: sequential container

In the UI you see a parent with two indented children. On disk
(`pages/website.md`):

```markdown
- TODO Website refresh
  plan-end:: 2026-07-31
  order:: sequential
  - TODO Design landing page
    duration:: 3h
    time-map:: deep-work
  - TODO Write copy
    duration:: 2h
    time-map:: deep-work
  - TODO Remember inspiration links
```

What Lodo sees:

- `Website refresh` has no `duration::` of its own in effect — it is a
  container (has Task descendants), so it never enters the ordering even if you
  add a `duration::` to it later. That line would be ignored.
- `Remember inspiration links` is a checklist item (no `duration::`): invisible
  to scheduling, does not make the parent a container by itself.
- `order:: sequential` chains the leaves in outline order: effective
  `Write copy depends on Design landing page`. Both are still placed upfront in
  one run (no one-at-a-time gating in v0.1).
- Delete the `order:: sequential` line and the two leaves become `parallel`:
  both draw on `deep-work` hours ordered only by `priority::` then outline
  order, and may interleave with other Tasks.

### Worked example: parallel project with inherited window

```markdown
time-map:: deep-work
plan-end:: 2026-10-31

- TODO Conference talk
  order:: parallel
  priority:: A
  - TODO Draft slides
    duration:: 4h
  - TODO Rehearse talk
    duration:: 2h
    depends:: ((11111111-2222-3333-4444-555555555555))
  - TODO Print handouts
    duration:: 1h
    time-map:: errands
```

What Lodo sees:

- All three leaves inherit `plan-end:: 2026-10-31` and `time-map:: deep-work`
  from the page; `Print handouts` overrides the map to `errands` and competes
  for different hours.
- `priority:: A` on the parent cascades to all three leaves (none sets its
  own), so all three outrank `priority:: B` Tasks for the same hours.
- The explicit `depends::` on `Rehearse` adds to — does not replace — the
  parallel default. If the parent were `sequential`, `Rehearse` would wait for
  *both* `Draft` (chain) *and* its explicit dep (AND).

### Worked example: DONE suppression

```markdown
- DONE Paused client work
  - TODO Draft proposal
    duration:: 5h
```

Nothing under `Paused client work` is scheduled and nothing depending on
`Draft proposal` via `depends::` proceeds (reported `blocked by …` naming the
suppressed Task). Flip the parent back to `TODO`/`DOING` and the subtree
re-enters the ordering. Marking `Draft proposal` itself `DONE` instead
satisfies its dependents immediately.

## Check feedback loop

- `lodo check`: `todo-missing-duration`, `bad-property-value`,
  `unknown-property`, `unknown-time-map`, plus `unknown-dependency` and
  `dependency-cycle`; shortfalls name `blocked by …`.
- `lodo forecast`: dry-run Blocks by day + `Shortfall:` with reasons
  (`blocked by incomplete dependencies`, `dependency cycle`, `unknown
  dependency`, `pin precedes dependency`).
- Fix in Logseq, re-run. No writes to the graph ever happen.
