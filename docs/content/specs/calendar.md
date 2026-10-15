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
- A machine with no browser finishes the same grant elsewhere:
  `lodo auth google --no-launch-browser` prints the authorization URL to complete on any
  device, and the redirect address Google then shows — or the code inside it — is pasted
  back on one line to exchange. The loopback `redirect_uri`, PKCE verifier and exchange
  are the same, so a tunnelled redirect completes the flow unchanged. Google's login is
  one system for personal and Workspace accounts; the account approved with is the one
  Lodo reads.

### Creating the OAuth client (one-time setup)

`lodo auth google` needs an OAuth client you own — Google has no shared Lodo
client. Create a **Desktop app** client in your own Cloud project:

1. Create a project — [Create a Google Cloud project](https://developers.google.com/workspace/guides/create-project).
2. Enable the Calendar API in it — [Enable Google Workspace APIs](https://developers.google.com/workspace/guides/enable-apis).
3. Configure the consent screen — [Configure the OAuth consent screen](https://developers.google.com/workspace/guides/configure-oauth-consent):
   user type **External**, add your personal Gmail as a test user, and add the
   `calendar.events` scope. The unverified-app warning at grant time is normal
   for a personal client.
4. Create the client — [Create access credentials](https://developers.google.com/workspace/guides/create-credentials)
   (section *Desktop app*). Its redirect URI is `http://127.0.0.1/`.
5. Paste the pair into the credentials file beside the Logseq token if present:

   ```json
   {"google": {"client_id": "…", "client_secret": "…"}}
   ```

6. Run `lodo auth google` and approve with the account whose calendar Lodo
   should use; `calendar.id` then names the one calendar inside it.

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
event becomes a Pinned Block and stays pinned at the user-chosen time and span — through unrelated changes to the Task's
own inputs, adopting the Task's current title — until its event is deleted. Both are treated as intent: Lodo does not
silently recreate work the user removed. `lodo check` lists stood-down Blocks so they stay
visible. Whether a Task's inputs have changed is judged against Lodo's snapshot of its
resolved inputs — direct and inherited properties, plus its text — taken on the run that
recorded the disposition.

Adoption is also what makes `schedule` writing the calendar **before** the store safe: a
crash between the two leaves `lodo=true` events with no local row, which the next run
adopts. See [ADR 0007](../ADRs/0007-write-the-calendar-before-the-store.md).

## Deferred

Auto-Defer; suggestions; writing completion back; syncing more than one calendar;
per-event reminders; Outlook and iCloud.
