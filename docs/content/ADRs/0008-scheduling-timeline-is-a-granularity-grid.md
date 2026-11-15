---
title: "The scheduling timeline is a granularity slot grid"
weight: 80
---

> Status: **accepted.**

Inside the planner, the Scheduling window is discretized into `scheduling.granularity`
slots, built day by day in `scheduling.timezone` so that 23- and 25-hour DST days yield
the right number of slots. Time Map windows, busy time from External Events, Pins and
Pinned Blocks all project onto this grid before any placement happens; a Block is a whole
number of slots.

A slot is local wall-clock time. A Time Map window authored as 09:00–11:00 still means
09:00–11:00 in `scheduling.timezone` after a daylight-saving change
([time-maps.md](../specs/time-maps.md)). The day-by-day grid is what keeps a short or
long DST day aligned with that clock.

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
