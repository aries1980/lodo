---
title: "Calendar writes fan out within delete, patch, insert phases"
weight: 250
---

> Status: **accepted.** Write ordering and crash recovery live in
> [calendar.md](../specs/calendar.md) and
> [ADR 0007](./0007-write-the-calendar-before-the-store.md).

`schedule` diffs the plan against the Managed events it observed and applies the
resulting deletes, patches, and inserts before committing the store. Each operation
targets one event id, so the operations are independent. They were still applied one
at a time, and a large re-plan paid one Google round-trip per changed Block.

The batch now runs in three phases: deletes, patches, then inserts. Each phase fans out
with at most four workers through `internal/fanout.Do`; later phases wait for earlier
ones. The phase barrier is not cosmetic. A Managed event whose private ownership
pointer is corrupt is deleted, while the plan can insert a Block whose deterministic id
is the same event id. If the insert ran first it would hit 409, fall back to patching
the event, and the delete would then remove it. Delete-before-insert keeps that
recovery path correct. Within a phase the event ids are distinct, so the work is safely
independent.

The first failure cancels the rest of that phase and stops later phases. The store is
not committed, the same rule as a sequential failure (ADR 0007). Operations already in
flight in the failed phase may still land; the next run reconciles them by deterministic
id and the `lodo-span` marker, exactly as it reconciles a crash between the calendar and
the store.

Applying every write at once was rejected because of the delete/insert collision above.
Strict sequential writes were the problem being solved. A per-event dependency graph
would encode the delete/insert edge generally, but the only edge is the malformed-pointer
case, and the phase order covers it with far less machinery. Unbounded writes were
rejected for the same reason as unbounded reads: the write quota and the calendar's
response are not a load target.

Consequences: `run.Calendar` is documented as safe for concurrent writes, and every
implementation (the Google client and test fakes) must keep that contract. A failed
phase can leave more of the batch applied than the sequential code would have; the
calendar remains the first write and reconciliation owns the repair. `Writes` in the
schedule outcome is still the planned batch, not the execution order; the stable
delete/patch/insert order is what the audit and the JSON output describe.
