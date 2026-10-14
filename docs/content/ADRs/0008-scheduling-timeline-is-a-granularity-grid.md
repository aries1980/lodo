---
title: "The scheduling timeline is a granularity slot grid"
weight: 80
---


Inside the planner, the Scheduling window is discretized into `scheduling.granularity`
slots, built day by day in `scheduling.timezone` so that 23- and 25-hour DST days yield
the right number of slots. Time Map windows, busy time from External Events, Pins and
Pinned Blocks all project onto this grid before any placement happens; a Block is a whole
number of slots.

This makes the properties the spec leans on exact rather than approximate: determinism
becomes an integer comparison, and "fewest, largest, contiguous Blocks" becomes a
run-length search over free slots. The cost is that busy time rounds conservatively onto
the grid — an event overlapping a slot blocks the whole slot — sacrificing up to one
granularity unit per event edge. That is accepted as consistent with granularity being
defined as the quantum of the Schedule.

The alternative — keeping continuous half-open intervals and quantizing only when a Block
is emitted — avoids the rounding loss, but pushes DST handling and run-length placement
into continuous-time arithmetic and makes "did anything change?" a floating-point
comparison.
