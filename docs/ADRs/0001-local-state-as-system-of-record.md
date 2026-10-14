---
title: "Local state is the system of record; calendars are projections"
weight: 10
---


Lodo is single-user and local-first, and its Logseq graph is the source of truth for
task *inputs*. But neither the graph nor the calendar can state what the scheduler
decided, so Lodo keeps the computed Schedule in a local store (SQLite): the binding
from each Task to its Blocks and to the calendar event IDs, plus the input snapshot
each was computed from. Google Calendar is treated as a projection of that store.

On refresh, Lodo reads the calendar back by event ID. Blocks Lodo created that were
moved by hand become Pinned Blocks that later schedules work around; events Lodo did
not create are External Events — busy time it never manages.

The alternatives were rejected deliberately. Deriving the Schedule solely from
calendar events loses the distinction between time Lodo scheduled and time the user
created, and cannot survive an event being deleted. Writing computed Blocks back into
the Logseq graph makes every reschedule a mutation of the user's notes and races with
the graph's own editor.
