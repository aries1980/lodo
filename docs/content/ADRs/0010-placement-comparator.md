---
title: "Placement maximises placed hours, then prefers fewer, larger, earlier Blocks"
weight: 100
---

> Status: **accepted.** Candidate rules and worked examples live in
> [scheduling.md](../specs/scheduling.md#placement-comparator).

[scheduling.md](../specs/scheduling.md) settles the order in which Tasks are considered
and leaves one Task's placement open. This record fixes that choice. Among the legal
arrangements of a Task's hours, Lodo uses one total order, so the planner is
deterministic and the trade-offs are explicit.

Candidates are compared lexicographically, best first:

1. **Placed slots, descending.** Completeness comes first. A shortfall is what a
   constraint produced.
2. **Start day, ascending.** Front-loading across days beats a tidier later day.
3. **Block count, ascending.** Within a day, fewer Blocks beat earlier fragments.
4. **Block start slots, ascending.** The earlier first Block wins.
5. **Block sizes, descending.** The earliest free run is filled as far as it can be.

The keys are total: two distinct candidates cannot tie, so the planner needs no further
tie-break. How a candidate is formed — whole runs, the two-slot floor, at most one
Block per run — is part of the scheduling spec, with the examples that pin this order.

Putting contiguity ahead of completeness was rejected. A 2h Task facing 1h15m of legal
free runs would then shrink to a single 45m Block to avoid a second fragment. Pins,
Pinned Blocks, and stood-down Blocks stay out of the comparison: they shape the free
slots before candidates exist. The function stays pure
([ADR 0006](./0006-schedule-is-a-pure-function-of-intent.md)). Preference tiers are a
filter in front of this order, not another key
([ADR 0014](./0014-preference-tiers-before-the-comparator.md)).
