---
title: "Tasks"
weight: 40
---


> Status: **draft (v0.1).** Definitions live in [`glossary.md`](./glossary.md).

A Task is a unit of work Lodo schedules. Tasks are Logseq blocks; the rest of the graph
is notes.

## What makes a block a Task

A block is a **Task** — i.e. eligible to be scheduled — **iff** it is marked `TODO` or
`DOING` **and** carries an explicit `duration::`.

Both halves are the gate:

- A `TODO` block **with** a duration is a Task and will be scheduled.
- A `TODO` block **without** a duration is a note; Lodo ignores it for scheduling and
  lists it in the `check` report so the omission is visible.
- Status uses Logseq's native markers: `TODO` / `DOING` are pending; `DONE` / `CANCELLED`
  are complete.

There is no `default duration` in v0.1. A default would resolve every `TODO` to a
duration and thereby reopen the gate, letting the Outline no longer be a safe place to
think.

## Properties

| Property | Meaning | Inherits? |
| --- | --- | --- |
| `duration::` | estimated effort (`90m`, `2h`, `1h30m`) | **no** |
| `time-map::` | one or more Time Map slugs, comma-separated | page, then parent blocks, then graph default |
| `priority::` | relative rank (`A` / `B` / `C`) | page, then parent blocks |
| `deadline::` | reference due date | **no** |
| `plan-start::` | earliest day to schedule | page, then parent blocks |
| `plan-end::` | day by which to complete | page, then parent blocks |
| `scheduled::` | the Pin: an exact date-time | **no** |
| `depends::` | finish-to-start predecessors (`((uuid)), ((uuid))`) | **no** |
| `order::` | `sequential` or `parallel` for this block's children | page, then parent blocks |

`deadline::` and `scheduled::` are Logseq's **native** properties, reused rather than
reinvented. `depends::` uses Logseq block references (`((` + autocomplete);
`order::` defaults to `parallel` when absent.

### Plan — *when by*

The presence of `plan-start::` and/or `plan-end::` expresses the Plan. There is no
`plan::` key.

- **Neither present → flexible.** Scheduled as soon as possible, ordered by Priority.
- **`plan-end::` only → complete by.** "By Friday."
- **`plan-start::` only → no earlier than.** "Not before the 1st."
- **Both → a window.** "Sometime 20th–25th."

Dates are **day-granular** (`2026-10-20`), never times. A specific *time* is a Pin.

`plan-end::` is **inclusive**: a Task with `plan-end:: 2026-10-20` may occupy any
qualifying hour up to end-of-day on 2026-10-20 in `scheduling.timezone`.
`plan-start::` is inclusive from start-of-day in `scheduling.timezone`.

**Overdue:** a time-bound Task whose `plan-end::` lies before today is overdue.
Overdue Tasks are reported and scheduled as soon as possible within their Time Maps,
like flexible Tasks with maximum urgency — never silently dropped and never stood down.

### Pin — *exactly when*

A `scheduled::` on the Task is a Pin. It overrides Plan and Priority: Lodo places the
Task at that moment and does not move it. Times belong to Pins; windows are days. A
Pin's time is used exactly as authored — it need not align with the scheduling
granularity — and it blocks every slot it overlaps.

### Time Map — *which hours of the week*

`time-map:: deep-work` or `time-map:: deep-work, weekends`. If absent, the value is
inherited from parent blocks (nearest first), then the enclosing page, then the
graph default (multi-graph `default_time_map` only), and failing that, the default
Time Map applies. Put `time-map:: company-a` on a project parent
block and every child TODO inherits it. A project Task prefers its
own map and spills into the default map at the same tier or worse when full. See
[`time-maps.md`](./time-maps.md).

### Priority — *ordering*

Reuses Logseq's native `priority::` (`A`/`B`/`C`). Priority is **relative**: among Tasks
competing for the same hours, higher priority is placed first. It only matters when tasks
compete; it does not override a Plan window or a Time Map.

### Deadline — *reference due*

A hard, externally-imposed due date, for the user's benefit and for filtering. It never
causes a Task to be placed — only a Plan does — and Lodo may schedule a Task after its
Deadline if that is what the Plan says. Reuses Logseq's native `deadline::`.

### Dependencies — *what waits for what*

`depends:: ((uuid)), ((uuid))` declares strict finish-to-start predecessors.
No Block of the dependent starts before all placed Blocks of every dependency end.
Multiple deps are AND (wait for the last finisher). A dependency on a `DONE` or
`CANCELLED` block is satisfied immediately; a ref to a missing UUID, a plain note,
or a `TODO` without `duration::` is an authoring error (`check` fails, dependent
blocked). Self-refs and cycles are hard stops: Tasks in the cycle are left
unscheduled and reported, never force-ordered. Malformed entries (bare UUIDs,
typos outside `(())`) are never guessed at.

If a dependency only partly fits, the dependent waits for the *full* dependency:
any shortfall blocks the dependent entirely. Stood-down hours count as unplaced
and block dependents until the dependency's inputs change. A `depends::` on a
container expands to all its leaf Tasks.

Plan wins over order: a dependency never pushes a Task past its `plan-end::`;
the dependent is left short with `blocked by …`. Pin wins over order: a
`scheduled::` stays at its authored time and the violation is reported.

### Nesting — containers and sequential chains (SkedPal parity)

Outline hierarchy is meaningful, SkedPal-style:

- A Task with Task descendants is a **container**: it is never scheduled itself
  and its own `duration::` is ignored (the sum of its leaves is the real effort).
  Checklist children are `TODO` blocks *without* `duration::` — notes, never
  scheduled, never making the parent a container.
- Non-Task parents are ignored for ordering. A `DONE`/`CANCELLED` parent
  suppresses its whole subtree (children unscheduled until it reopens).
- `order:: sequential | parallel` (default `parallel`) on a parent chains its
  descendant leaf Tasks in outline order (each waits for the previous).
  Sequential cascades: an overarching sequential parent chains all leaves below
  it. There is no one-at-a-time gating in v0.1 — sequential chains are all
  scheduled upfront in order.
- `time-map::`, `priority::`, `plan-start/end::` and `order::` cascade
  explicit child > parent blocks (nearest first) > page > default.
  `duration::`, `deadline::`, `scheduled::` and `depends::` never inherit.

## Splitting into Blocks

A Task's duration may span several Blocks when one contiguous window cannot hold it.

- Blocks are whole multiples of the **scheduling granularity** and round **up**; a
  5-minute Task at 15-minute granularity occupies one 15-minute Block.
- The splitting policy is **fewest, largest, contiguous Blocks first**. Lodo fragments a
  Task only as far as the Plan window and Time Maps force it. When earliest fragmented
  placement competes with a later contiguous placement, contiguity wins within the same
  day — Lodo prefers fewer, larger Blocks over strictly earliest fragments.
- **Minimum Block floor:** Lodo never emits a Block smaller than two granularity units
  (e.g. 30m at 15m granularity). Hours that would fragment below the floor are left
  unplaced and reported as shortfall rather than emitted as calendar confetti. The floor
  governs *fragments*: a Task whose whole duration rounds up to a single granularity
  unit is emitted as that one unit.

## Deferred

Multiple Plans per Task; recurring Plans and habits; sequential one-at-a-time
gating; Minimum Block Length as a user property; Bundling; Buffers;
Auto-Defer; Zones and budgets.
