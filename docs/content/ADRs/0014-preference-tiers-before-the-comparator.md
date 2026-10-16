---
title: "Preference tiers filter free slots before the placement comparator"
weight: 140
---

Time Map windows already carry `green`, `yellow`, or `red`, and an absent tier
word means green. Placement now uses that word. Hours still required are
searched in three passes: green, then yellow, then red. Each pass runs the
[ADR 0010](./0010-placement-comparator.md) comparator on that tier's free
runs. A slot covered by two windows takes the better tier. A Block stays
inside one tier. What remains after red is a shortfall.

Tier outranks front-loading across tiers. A Task with green on Friday and
yellow on Monday is placed on Friday. Inside one tier, earlier days still win.

The two-slot minimum Block floor applies per pass. A one-slot green fragment
is not emitted, and those hours may spill into a later tier.

## Alternatives

- Fold the tier into the ADR 0010 comparator as another key. Rejected: one
  search can then trade a green hour for an earlier yellow hour, or draw a
  Block across a tier boundary.
- Treat yellow and red as forbidden. Rejected: the authored rule is to spill,
  and to report a shortfall only when even red cannot hold the Task.

## Consequences

- A map that uses yellow or red changes placement on the next `schedule`.
  A map whose windows are all green (the default) places as before.
- A flexible Task may sit later in the week in order to stay in green.
