---
title: "Local state is the system of record; calendars are projections"
weight: 10
---

> Status: **accepted.**

Lodo is single-user and local-first. Task inputs are authored in Logseq or in the
TUI ([ADR 0017](./0017-tui-edits-logseq-blocks-on-explicit-save.md)). A TUI save
writes the block into the graph, and that graph is what a later run reads. Neither
the graph nor the calendar can state what the scheduler decided, so Lodo keeps the
computed Schedule in a local store (SQLite): the binding from each Task to its
Blocks and to the calendar event IDs, plus the input snapshot each was computed
from. Google Calendar is a projection of that store.

On refresh, Lodo reads the calendar back by event ID. Blocks Lodo created that were
moved by hand become Pinned Blocks that later schedules work around; events Lodo did
not create are External Events — busy time it never manages.

The alternatives were rejected deliberately. Deriving the Schedule solely from
calendar events loses the distinction between time Lodo scheduled and time the user
created, and cannot survive an event being deleted. Writing computed Blocks back into
the Logseq graph makes every reschedule a mutation of the user's notes and races with
the graph's own editor. That rejected write is a computed Schedule. Saving a block
the user edited in the TUI is a different write
([ADR 0017](./0017-tui-edits-logseq-blocks-on-explicit-save.md)).
