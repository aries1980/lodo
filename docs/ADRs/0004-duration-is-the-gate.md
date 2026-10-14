---
title: "Scheduling is gated by duration, and plans are optional date bounds"
weight: 40
---


A block becomes schedulable (a Task) when it is `TODO`-marked and carries an explicit
`duration::`. A Task with no `plan-start::`/`plan-end::` is scheduled as soon as
possible, ordered by priority; date bounds, if present, merely constrain the window.

This deviates from SkedPal deliberately. SkedPal requires an explicit *plan* before a
line is scheduled, which is what lets its Outline be a safe place to dump ideas. Lodo
collapses "plan" into optional date bounds and moves the gate onto `duration::`, so a
TODO without a duration is simply a note.

The alternative — a configured *default* duration — was rejected because it resolves
every TODO to a duration and therefore removes the gate entirely, so the Outline would
flood the calendar with work the user never chose to schedule.

Consequences: there is no `plan::` keyword and no default duration. Lodo must report
TODO blocks that lack a duration, so that an omission is visible rather than silent. If
the friction of always typing a duration proves real, a default plus an explicit
opt-out property is a later, additive change.
