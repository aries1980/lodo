---
title: "Google Tasks API is out of scope; Calendar Events are the sole projection"
weight: 120
---

Lodo does not read or write Google Tasks. The single projection remains Calendar
Events on the one calendar named by `calendar.id`
([`calendar.md`](../specs/calendar.md)).

The Tasks API manages task lists: title, notes, and a due date, with no start,
end, or duration. It cannot represent a Block, one contiguous interval of
scheduled time ([`glossary.md`](../specs/glossary.md)), so anything written there would be a
second, lossy projection of the same Schedule kept in SQLite
([`0001-local-state-as-system-of-record.md`](./0001-local-state-as-system-of-record.md)).

## Alternatives

- Mirror unscheduled work or shortfall as a task list: rejected, `check`
  already reports it, and Logseq stays the only interface.
- Write completion back via Tasks: rejected, completion lives in Logseq
  (`DONE`/`CANCELLED`); Calendar reconciliation already reads user intent
  (moves become Pinned Blocks, deletes stand Blocks down).
- Companion mobile view via Google Tasks: rejected, Calendar already renders
  Managed Events; a second writable surface doubles reconciliation
  (identity, pin/stand-down, adoption on state loss) and needs a further
  `tasks` OAuth scope for no scheduling gain.

## Consequences

- No `tasks` scope, no task-list bookkeeping in the store, no changes to the
  busy-time or reconciliation rules in [`calendar.md`](../specs/calendar.md).
- Calendar UI merging Tasks and Events makes a dual write actively confusing:
  the same Task would appear twice with different semantics.
- Revisiting this needs a new record, since it breaks the one-calendar
  projection and the calendar-first crash recovery in
  [`0007-write-the-calendar-before-the-store.md`](./0007-write-the-calendar-before-the-store.md).
