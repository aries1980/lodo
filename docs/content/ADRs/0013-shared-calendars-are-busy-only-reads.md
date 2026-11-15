---
title: "Shared calendars are busy-only reads; one calendar stays the write target"
weight: 130
---

> Status: **accepted.** Busy-time rules and per-calendar failures live in
> [calendar.md](../specs/calendar.md). The config list lives in
> [configuration.md](../specs/configuration.md).

Calendars shared with the user contribute busy time. One calendar, named by
`calendar.id`, stays the only write target. `calendar.read_calendars` names the busy
sources. A read calendar is read-only as Lodo's own policy. Lodo does not write, move,
or delete there, even when the Google share grants write.

Busy time is the union of the write calendar and every read calendar inside the
Scheduling window. Detecting a Managed Event stays on the write calendar. A
`lodo=true` event on a read calendar is ordinary busy time plus a warning. It is not
adopted, moved, or stood down.

A 403 or 404 on one read calendar warns, and the run continues with the calendars that
answered. Auth failures and network failures still fail the run. The 403 warning covers
revoked access, a share that was never accepted, and a free/busy-only share in one
text. Lodo does not call `calendarList` to split those causes apart. Reading a
free/busy-only share through `freeBusy.query` is deferred: that is a second read shape
for a share the owner can upgrade to reader.

One writable calendar per share was rejected. Identity, pin, stand-down, and adoption
would fan out per calendar, and the Schedule would not get better.

A skipped read calendar leaves a hole in the busy mask, and a later `schedule` can
place work into time that calendar held. The run says which calendars were read.
Shared all-day events block whole days unless configuration drops them. The write
calendar stays busy for all-day events, because there the user can mark the event free.
