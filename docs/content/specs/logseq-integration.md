---
title: "Logseq Integration"
weight: 80
---


> Status: **draft (v0.1).**

## Direction: read-only

In v0.1 Lodo **only reads** the Logseq graph. It never writes Tasks, Blocks, or
properties. The Schedule lives in SQLite and is projected onto the Calendar; the graph
is the source of truth for *intent* and nothing else.

This is deliberate. Logseq is a second writer to the same graph: writes made behind a
running Logseq can be overwritten or ignored, and a written-back schedule would have to
be distinguished from the user's own edits. See
[ADR 0002](../ADRs/0002-logseq-via-http-api-only.md) and the write-back rejection in
[ADR 0001](../ADRs/0001-local-state-as-system-of-record.md).

If a read-only projection into the graph is ever added, it will live under a `lodo-*`
property namespace, never `scheduled::`/`deadline::`/`duration::`, and will never be
read back as intent.

## Access

Exclusively through the **Logseq HTTP API**, via a per-flavour adapter (file-based graph
today, DB graph after the planned upgrade). The API URL comes from `config.yaml`; the
token from `credentials.json`. See [`configuration.md`](./configuration.md).

Reads are performed fresh on each run. Lodo's SQLite store keeps a snapshot of the
inputs it last read, so it can tell which Tasks changed without diffing the whole graph.

## What Lodo reads

| Source | Read as |
| --- | --- |
| Blocks marked `TODO` / `DOING` | candidate Tasks ([`tasks.md`](./tasks.md)) |
| Block properties | `duration::`, `time-map::`, `priority::`, `deadline::`, `plan-start::`, `plan-end::`, `scheduled::`, `depends::`, `order::` |
| Page properties | inherited defaults for `time-map::`, `priority::`, `plan-start::`, `plan-end::`, `order::` |
| Parent blocks | cascade for `time-map::`, `priority::`, `plan-start::`, `plan-end::`, `order::` (nearest ancestor wins); hierarchy for containers, sequential chains, DONE-suppression |
| The `Time Maps` page | Time Map definitions ([`time-maps.md`](./time-maps.md)) |

`DONE` and `CANCELLED` blocks are complete and are not scheduled. A `DONE`/`CANCELLED`
parent suppresses its whole subtree; a `DONE`/`CANCELLED` block satisfies any
`depends::` pointing at it.

## Reading semantics

- The `Time Maps` page name is configurable (`logseq.time_maps_page`, default
  `Time Maps`), so a namespaced page (`lodo/time-maps`) is a one-line change rather than
  a format decision.
- Parsing is permissive about whitespace and case in property keys; slugs are
  case-insensitive. `depends::` is strict: comma-separated `((uuid))` block
  references only, duplicates collapsed; bare UUIDs and typos are errors that
  block the dependent.
- Anything Lodo cannot parse is reported by `lodo check` and skipped, never guessed at.
  New graph issue kinds: `unknown-dependency`, `dependency-cycle` (plus
  `bad-property-value` for malformed `depends::`/`order::`).
