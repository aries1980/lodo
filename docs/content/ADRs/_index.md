---
title: Decision Records
weight: 20
---

The decisions worth remembering, with reasons. Each record was hard to reverse, surprising
without context, and the result of a real trade-off.

Specs say what Lodo does now. These records say why. When the two disagree, the record
is marked amended or the spec is corrected, and the page that changed says which.

## Report, do not repair

Several records are one rule. Lodo reports a constraint it cannot meet and leaves the
user's notes and calendar alone:

- A `TODO` without `duration::` is a note, not a Task with a guessed length
  ([ADR 0004](./0004-duration-is-the-gate.md)).
- A dependency cycle is a hard stop
  ([ADR 0011](./0011-dependencies-and-nesting.md)).
- A missing Time Maps page is offered, and written only on an explicit yes
  ([ADR 0015](./0015-bootstrap-missing-time-maps-on-confirmation.md)).
- A shortfall suggestion is advice. Nothing in v0.1 applies it
  ([scheduling](../specs/scheduling.md#infeasibility)).

## Ownership of truth

- [ADR 0001](./0001-local-state-as-system-of-record.md) — local state is the system of record; calendars are projections.
- [ADR 0005](./0005-config-and-secrets-outside-the-graph.md) — configuration and secrets live outside the graph.
- [ADR 0007](./0007-write-the-calendar-before-the-store.md) — a run writes the calendar before the store.
- [ADR 0019](./0019-calendar-edits-are-user-intent.md) — a calendar edit is user intent.

## Graph

- [ADR 0002](./0002-logseq-via-http-api-only.md) — Logseq is reached only through its HTTP API.
- [ADR 0003](./0003-time-maps-in-graph-format.md) — Time Maps are defined in the graph.
- [ADR 0004](./0004-duration-is-the-gate.md) — duration is the gate; plans are optional date bounds.
- [ADR 0015](./0015-bootstrap-missing-time-maps-on-confirmation.md) — a missing Time Maps page is offered, never assumed. Amended by ADR 0017.
- [ADR 0017](./0017-tui-edits-logseq-blocks-on-explicit-save.md) — the TUI is an interface for intent, and it edits Logseq blocks on explicit save.
- [ADR 0018](./0018-deadline-is-a-label.md) — a deadline is a label, not a constraint.
- [ADR 0020](./0020-task-identity-is-the-block-uuid.md) — a Task's identity is its Logseq block UUID.
- [ADR 0021](./0021-each-graph-is-its-own-logseq-client.md) — each graph is its own Logseq client.

## Planning

- [ADR 0006](./0006-schedule-is-a-pure-function-of-intent.md) — the Schedule is a pure function of current intent.
- [ADR 0008](./0008-scheduling-timeline-is-a-granularity-grid.md) — the scheduling timeline is a granularity slot grid.
- [ADR 0010](./0010-placement-comparator.md) — placement maximises placed hours, then prefers fewer, larger, earlier Blocks.
- [ADR 0011](./0011-dependencies-and-nesting.md) — dependencies are strict finish-to-start, with nesting.
- [ADR 0014](./0014-preference-tiers-before-the-comparator.md) — preference tiers filter free slots before the placement comparator.
- [ADR 0022](./0022-outline-order-is-a-priority.md) — outline order is the last priority key.

## Calendar projection

- [ADR 0012](./0012-google-tasks-api-out-of-scope.md) — Google Tasks is out of scope; Calendar Events are the sole projection.
- [ADR 0013](./0013-shared-calendars-are-busy-only-reads.md) — shared calendars are busy-only reads.

## Runtime

- [ADR 0009](./0009-lodo-is-a-single-go-binary.md) — Lodo is a single Go binary.
- [ADR 0016](./0016-dual-output-json-logs-and-colors.md) — JSONL logs for machines, colors for humans.
- [ADR 0023](./0023-json-wire-schema-boundary.md) — one versioned wire schema for command output.
