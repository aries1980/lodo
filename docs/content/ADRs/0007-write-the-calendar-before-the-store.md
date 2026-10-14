---
title: "Apply writes the calendar before the store"
weight: 70
---


Lodo writes to two systems with no shared transaction: Google Calendar and its SQLite
store. Every `schedule` writes the calendar first and commits the store second. A crash
between the two therefore leaves `lodo=true` events with no local row — which
reconciliation reads as "Lodo's state was lost" and adopts, after which the next run
reproduces the same times from the same intent.

The reverse order is a trap. Committing the store first leaves rows whose Managed Events
are missing, and reconciliation reads a missing Managed Event as the user having deleted
it — so a transient crash would silently stand Tasks down. Writing calendar-first is what
makes the adoption rule load-bearing for crash recovery rather than a mere convenience,
and is why it must not be "simplified" away.

Every individual write is idempotent under replay because event IDs are deterministic,
derived from the Task's block UUID and Block index. An event whose computed instant
already matches the store is left untouched, so a no-op run issues no writes.
