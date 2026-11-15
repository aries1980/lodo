---
title: "A deadline is a label, not a constraint"
weight: 180
---

> Status: **accepted.**

`deadline::` is Logseq's native due date. Lodo keeps it so a person can see it and
filter on it. It does not place a Task, it does not order Tasks, and a Schedule may
sit after it when that is what the Plan says. Only `plan-start::` and `plan-end::`
bound the window ([ADR 0004](./0004-duration-is-the-gate.md)).

Treating a deadline as `plan-end::` was rejected. A date copied from a meeting or an
outside promise would then become a hard window the person never authored as a Plan,
and a Task with only a deadline would stop being flexible. Treating it as urgency, a
sort key beside `plan-end::`, was rejected for the same reason. It would schedule work
the Plan did not ask for, and it would hide the difference between "I intend to finish
by Friday" and "someone else's date is Friday".

A Task with a deadline and no Plan stays flexible. `explain` can show the date while
the planner leaves it unused. Promoting deadlines into the Plan is a new decision.
