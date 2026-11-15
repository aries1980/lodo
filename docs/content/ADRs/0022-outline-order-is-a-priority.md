---
title: "Outline order is the last priority key"
weight: 220
---

> Status: **accepted.**

When a Pin, dependencies, Plan bounds, and `priority::` do not separate two Tasks,
outline order decides who is considered first. Outline order is the depth-first,
top-to-bottom order of blocks as Logseq displays them and as the HTTP API returns
them. Moving a block in the notes changes that order, and so changes who wins the tie.

A numeric rank was rejected for v0.1. Logseq already has `priority::` with three
levels, and another number would be a property invented for a tie that outline order
already breaks deterministically. Leaving the tie unstable was rejected. The Schedule
would then depend on map iteration, and
[ADR 0006](./0006-schedule-is-a-pure-function-of-intent.md) would not hold.

The re-prioritisation is visible. `forecast`, `schedule --verbose`, and `lodo explain`
say when outline order decided placement. A later numeric rank would be an added
property. This fallback stays until that record exists.
