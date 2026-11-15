---
title: "Dependencies are strict finish-to-start with SkedPal-parity nesting"
weight: 110
---

> Status: **accepted.** Property rules live in [tasks.md](../specs/tasks.md).

Explicit `depends:: ((uuid))` edges and parent/child nesting are one slice. The order
is strict finish-to-start: a dependent waits until every dependency's placed Blocks
have ended. Several dependencies are AND. Any shortfall, and any stood-down Block,
blocks the dependent entirely. `DONE` and `CANCELLED` satisfy a dependency. A missing
target, a note, or a `TODO` without a duration is an error and blocks. A cycle,
including a self-reference, is a hard stop. The Tasks in it stay unscheduled, and the
cycle is reported.

A Task with Task descendants is a container. It is not scheduled, and its own duration
is ignored. `order:: sequential` chains descendant leaves in outline order, all of them
up front. `order:: parallel` leaves them unordered. `time-map`, `priority`, `plan-*`,
and `order` cascade from the child, then parent blocks, then the page. `depends` does
not inherit. A `DONE` parent suppresses its subtree.

The Plan wins over the chain. A dependency does not push a Task past `plan-end::`; the
shortfall is reported. A Pin wins over the chain. A `scheduled::` stays at its authored
time, and the violation is reported. Among unrelated Tasks the usual consideration
order is the tie-break.

Soft ordering, where a dependent is merely considered later and may overlap, was
rejected. "Draft before review" is not a rule a person can check if the two overlap.
Scheduling a container for the sum of its children was rejected. The leaves are the
work, and scheduling the parent as well double-books them. Breaking a cycle by outline
order was rejected. That would drop an explicit order and report nothing. SkedPal's
one-at-a-time sequential gating, and gap fields, stay deferred.

A change to dependencies or nesting is a change to the Task's inputs, so it can release
a stood-down Block. The resolver reports cycles and unknown dependencies. The planner
reports a Block left unplaced because a dependency is unmet.
