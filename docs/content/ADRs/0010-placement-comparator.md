---
title: "Placement maximises placed hours, then prefers fewer, larger, earlier Blocks"
weight: 100
---


[`scheduling.md`](../specs/scheduling.md) settles the order in which Tasks are considered
but leaves the placement objective open. This record fixes how one Task's placement is
chosen among the candidate arrangements of its hours, so the planner is deterministic
and its trade-offs are explicit rather than accidental.

## Candidates

A Task needs `n = ceil(duration / granularity)` slots. A *candidate placement* is an
arrangement of at most `n` slots into Blocks, drawn from the Task's free slots:

- each Block is a run of consecutive free slots — free meaning inside the Task's Time
  Maps and Plan window, not in the past, and not occupied by busy time, Pins, or Pinned
  Blocks;
- at most one Block per free run, starting at the run's first slot;
- each Block is at least two granularity units (the Minimum Block floor), except a Task
  whose whole duration rounds up to a single unit (`n = 1`), which is emitted as one
  unit;
- the Blocks' sizes sum to at most `n`.

## Comparator

Candidates are compared lexicographically, best first:

1. **Placed slots, descending.** Completeness first: Lodo places every hour it legally
   can. Shortfall is the outcome of a constraint, never a preference.
2. **Start day, ascending.** Front-loading across days: of two placements, the one that
   begins earlier is preferred — even the more fragmented one.
3. **Block count, ascending.** Within a day, contiguity wins: between placements that
   start the same day and place the same hours, fewer Blocks beat earlier fragments.
4. **Block start slots, ascending.** The earlier first Block wins.
5. **Block sizes, descending (lexicographic).** Largest-first: the earliest run is
   filled as far as possible before later runs are used.

The keys are total: two distinct candidates cannot tie on all of them, so the planner
needs no further tie-break.

## Worked examples

At 15m granularity, one day unless stated; runs written as `time@length`.

| n | Free runs | Winner | Because |
| --- | --- | --- | --- |
| 2 | 09:00@15m, 13:00@15m | nothing placed | Fragments below the floor are never emitted; 30m of shortfall is reported. |
| 4 | 09:00@30m, 13:00@45m | 09:00@30m + 13:00@30m | Completeness: 60m placed beats one 45m Block, because the extra 30m is a legal Block, not confetti. |
| 5 | 09:00@30m, 13:00@45m | 09:00@30m + 13:00@45m | The only placement that places all 75m. |
| 5 | 09:00@30m, 13:00@45m, 17:00@75m | 17:00@75m | Same-day contiguity: equal completeness, so one Block beats two that start earlier. |
| 6 | 09:00@60m, 13:00@60m | 09:00@60m + 13:00@30m | Largest-first: equal placement, so the earliest run is filled as far as possible. |

Across days, a 75m Task with only 30m available on day one and 75m available on day two
is placed as 30m on day one plus 45m on day two: contiguity wins only within the same
day, so front-loading takes the due it is owed.

## Consequences

- The planner remains a pure, deterministic function: the comparator is a total order
  over finitely many candidates, so identical inputs always select an identical
  placement ([ADR 0006](./0006-schedule-is-a-pure-function-of-intent.md)).
- [`scheduling.md`](../specs/scheduling.md)'s "contiguity wins within the same day" is
  read as a rule among placements of equal completeness; completeness outranks it.
  Without that reading, a 2h Task facing 1h15m of free runs would be cut down to a
  single 45m Block to avoid a second fragment.
- Pins, Pinned Blocks and stood-down Blocks never reach the comparison: they shape the
  free slots and the required `n` before candidates are formed.
