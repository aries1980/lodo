---
title: "Shared calendars are busy-only reads; one calendar stays the write target"
weight: 130
---

Google Calendars shared with the user contribute busy time. One calendar
stays the single write target (`calendar.id`); `calendar.read_calendars`
names shared busy sources. Read calendars are Lodo-policy read-only: never
written, moved, or deleted, even when the Google share grants write.

## Decisions

- Busy is the union over the write plus every read calendar within the
  Scheduling window. The same rules apply everywhere — busy unless
  cancelled, self-declined, or transparent; all-day counts as busy;
  recurrences expand via `singleEvents` — except Managed-event detection,
  which is write-calendar-only, and except all-day events on a read
  calendar when `calendar.ignore_read_all_day` or
  `ignore_read_all_day_matching` drops them.
- A `lodo=true` event on a read calendar is ordinary busy time plus a
  `check` warning naming the event. Never adopted into SQLite, never moved,
  never stood down. Write-calendar reconciliation stays the sole owner of
  `lodo=true` semantics.
- Per-calendar 403/404 warns and continues in config order; auth and network
  failures still fail the run. The 403 warning covers all denied causes in
  one text — revoked, never accepted, or free/busy-only share — with no
  `calendarList` lookup. The 404 warning is shorter (removed calendar).
- Free/busy-only (`freeBusyReader`) shares need reader access: deferred,
  since they do not serve `events.list`.
- Config hygiene at load: trim, drop empties, silently drop any read entry
  equal to `calendar.id`, drop exact duplicates. The repeatable
  `--read-calendar` flag replaces the list for one run when given at least
  once.
- Audit: SQLite stores the write calendar plus per-read status
  (`read`, `skipped:403`, `skipped:404`) and event counts; `forecast`,
  `schedule`, and `refresh` print a `busy sources:` line plus one warning
  per skip. `refresh` additionally warns about Managed Blocks overlapping
  shared busy time and changes nothing. `check` validates shape only;
  reachability surfaces in the planning commands.

## Alternatives

- One writable calendar per share: rejected — identity, pin/stand-down, and
  adoption would fan out per calendar for no scheduling gain.
- `calendarList` lookup to distinguish 403 causes: rejected — an extra call
  per skip to word a warning nobody acts on differently.
- Free/busy-only via `freeBusy.query`: deferred — a second read shape for a
  share the owner can upgrade to reader in one click.

## Consequences

- Partial-mask scheduling after a skip can double-book the unreadable
  calendar. Mitigated by the printed `busy sources:` line, per-calendar
  warnings, and the stored run record.
- Shared all-day events block whole days unless config drops them
  (`ignore_read_all_day`, or `ignore_read_all_day_matching` for phrases such
  as a birthday). The write calendar stays busy for all-day events, because
  there the user can mark the event free.
