---
title: "Calendar Integration"
weight: 70
---


> Status: **draft (v0.1).**

Google Workspace Calendar only in v0.1. Outlook and iCloud are out of scope. There is
exactly **one** output Calendar (the one named by `calendar.id`); Time Maps are not
calendars.

## Auth

- **Loopback OAuth** as the user, one-time, via `lodo auth google`. The refresh token is
  written to `credentials.json` (`0600`), never into the graph.
- Scope: `https://www.googleapis.com/auth/calendar.events` (read and write events).
- The target calendar is `calendar.id` from `config.yaml` (default `primary`).

## Identifying a Managed Event

Ownership is recorded on the event itself, so it survives Lodo losing its local state:

- Every Managed Event carries **`extendedProperties.private`**:
  `lodo=true`, `task=<Logseq block UUID>`, `block=<index>`.
- Every Managed Event is inserted with a **client-supplied deterministic `id`**
  (base32hex, derived from the Task's block UUID and block index), so re-inserting is
  idempotent and cannot duplicate.
- The **title is the Task's own text**, unmarked — the block looks natural in Google.
- **No title markers, no separate calendar.**

The ownership test is therefore: *an event is Managed iff it carries `lodo=true`.*
Everything else on the calendar is an **External Event**: busy time, never moved.

### Task identity

A Task is identified across runs by its **Logseq block UUID**, not its title. Renaming a
Task does not orphan its Blocks or its calendar events.

## Event shape

```json
{
  "id": "lodo<base32hex>",
  "summary": "Write deep report",
  "start": { "dateTime": "2026-10-20T09:00:00+01:00", "timeZone": "Europe/London" },
  "end":   { "dateTime": "2026-10-20T11:00:00+01:00", "timeZone": "Europe/London" },
  "extendedProperties": {
    "private": { "lodo": "true", "task": "<logseq-block-uuid>", "block": "0" }
  },
  "reminders": { "useDefault": true }
}
```

Times use `scheduling.timezone`. Reminders use the calendar's defaults.

## Busy time

Lodo reads external events within the Scheduling window to compute availability:

- **Busy** = any event that is not Managed, not `cancelled`, not declined by the user, and
  whose `transparency` is not `transparent`.
- **All-day** events count as busy. (Known rough edge: a birthday will block the day. It
  can be marked free in Google. Revisit if it hurts.)
- Recurring events contribute their expanded instances.

## Reconciliation on `refresh`

Every `schedule` reconciles first. The rules:

| Observed | Meaning | Lodo's response |
| --- | --- | --- |
| Managed event, time unchanged | projection intact | nothing |
| Managed event, time changed | user moved it | becomes a **Pinned Block**; Lodo works around it |
| Managed event missing | user deleted it | that **Block** becomes **stood down**; not recreated until its Task's inputs change. Surviving Blocks stay projected |
| `lodo=true` event with no SQLite row | Lodo's state was lost | adopted into SQLite |
| Event without `lodo=true` | an External Event | busy time; never moved |

Stood-down is **per-Block**: deleting a Managed Event stands down only that Block,
not the whole Task. The Task's remaining Blocks stay projected and future runs may place
the missing hours elsewhere once the Task's inputs change. A moved
event becomes a Pinned Block and stays pinned at the user-chosen time — through unrelated changes to the Task's
own inputs, adopting the Task's current duration and title — until its event is deleted. Both are treated as intent: Lodo does not
silently recreate work the user removed. `lodo check` lists stood-down Blocks so they stay
visible.

Adoption is also what makes `schedule` writing the calendar **before** the store safe: a
crash between the two leaves `lodo=true` events with no local row, which the next run
adopts. See [ADR 0007](../ADRs/0007-write-the-calendar-before-the-store.md).

## Deferred

Auto-Defer; suggestions; writing completion back; syncing more than one calendar;
per-event reminders; Outlook and iCloud.
