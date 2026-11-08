---
title: "Apply writes the calendar before the store"
weight: 70
---


Lodo writes to two systems with no shared transaction: Google Calendar and its SQLite
store. Every `schedule` writes the calendar first and commits the store second. A crash
after an insert leaves a `lodo=true` event with no local row — which reconciliation
reads as "Lodo's state was lost" and adopts, after which the next run reproduces the
same times from the same intent.

A crash after a patch is the harder case: the local row exists but still carries the
old span, so the observed change could look like a user move. Every Lodo insert and
patch therefore stamps `extendedProperties.private.lodo-span` with the span it
intended. Reconciliation only turns a changed span into a Pinned Block when the
marker does not match that span; a matching marker is Lodo's own delayed write, so
the binding simply follows it.

The reverse order is a trap. Committing the store first leaves rows whose Managed Events
are missing, and reconciliation reads a missing Managed Event as the user having deleted
it — so a transient crash would silently stand Tasks down. Writing calendar-first is what
makes the adoption rule load-bearing for crash recovery rather than a mere convenience,
and is why it must not be "simplified" away.

Every individual write is idempotent under replay because event IDs are deterministic,
derived from the Task's block UUID and Block index. An event whose computed instant
already matches the store is left untouched, so a no-op run issues no writes.
