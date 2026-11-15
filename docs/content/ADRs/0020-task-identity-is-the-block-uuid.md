---
title: "A Task's identity is its Logseq block UUID"
weight: 200
---

> Status: **accepted.**

A Task is the same Task across runs because of its Logseq block UUID. The title is
not the identity, and neither is the position in the outline. Renaming a Task, or
moving it on the page, does not orphan its Blocks or its Managed Events. Event ids
and store keys are derived from that UUID and the Block index
([ADR 0007](./0007-write-the-calendar-before-the-store.md)).

Identifying a Task by its title was rejected. A reworded line would look like a new
Task, the old calendar event would be left behind, and the new line would be
scheduled again. Identifying it by outline position was rejected for the same reason.
Reordering notes is not deleting work.

Two graphs that report the same Task UUID are refused before a run writes anything.
A cloned graph would otherwise share event ids and store keys with the original, and
one Schedule would update the other's events. A block ref cannot name which graph it
means; cross-graph dependencies stay unsupported
([ADR 0021](./0021-each-graph-is-its-own-logseq-client.md)).
