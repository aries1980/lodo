---
title: "A calendar edit is user intent"
weight: 190
---

> Status: **accepted.** The reconciliation table lives in
> [calendar.md](../specs/calendar.md#reconciliation-on-refresh).

On the write calendar, a change to a Managed Event is the user telling Lodo what to
do with that Block. It outranks the planner until the Task's own inputs change.

A move pins the Block at the time and the span the user left. Later runs work around
that span. The title still follows the Task's current text, so a rename in Logseq
shows up on the pinned event. A delete stands that Block down. Lodo does not recreate
it until the Task's inputs change, and sibling Blocks of the same Task stay projected.
An event that has already ended expires the same way: its past Block stands down
rather than being recreated later.

The event list is bounded to the Scheduling window, so a missing row inside that list
is not proof of deletion. A binding outside the list is fetched by id. An event that
still exists stays where the user put it and becomes a Pinned Block, including past
the window. An event that is gone stands its Block down.

[ADR 0007](./0007-write-the-calendar-before-the-store.md) is the other half. Writes hit
the calendar first, and a `lodo-span` marker keeps Lodo's own crash from looking like
one of these edits. This record is about edits the user actually made.

Reasserting the planner over a moved or deleted event was rejected. The calendar would
stop being a place the user can correct a Block, and the next `schedule` would undo
the correction. Treating a delete as a one-run skip was rejected. The Block would come
back on the following run, which is the same as ignoring the delete.

`refresh` folds these edits into the store and does not recompute. `schedule`
reconciles first, then plans around the pins and the stood-down Blocks. Whether the
Task's inputs have changed is judged against the snapshot of resolved inputs — direct
and inherited properties, plus the text — taken on the run that recorded the
disposition.
