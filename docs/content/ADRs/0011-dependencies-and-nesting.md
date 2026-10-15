---
title: "Dependencies are strict finish-to-start with SkedPal-parity nesting"
weight: 110
---

Task dependencies plus parent/child nesting were deferred in scope. This record
fixes both as one slice: explicit `depends:: ((uuid))` edges plus containers
and `order:: sequential|parallel` chains, SkedPal-style.

## Decisions

- `depends::` is a strict comma `((uuid))` list (block refs, never typed UUIDs).
  Finish-to-start on placed Blocks; AND over multiple deps; full-gate (any
  shortfall blocks the dependent); stood-down counts as unplaced.
- `DONE`/`CANCELLED` satisfy; missing, notes, and `TODO`-without-duration are
  `unknown-dependency` errors that block. Malformed entries are
  `bad-property-value` and block. Cycles (including self) are hard stops, never
  silently broken (unlike SkedPal, which ignores them).
- Topological order overrides time-bound/flexible buckets; the existing
  comparator is the tie-break among unrelated Tasks. Plan wins (never slide past
  `plan-end::`, report `blocked by …`); Pin wins (stay + report violation).
- Nesting: a Task with Task descendants is a container (unscheduled, duration
  ignored); checklist = `TODO` without `duration::`. `order:: sequential`
  chains descendant leaves in outline order (all upfront, no one-at-a-time).
  `time-map/priority/plan-*/order` cascade child > parents > page;
  `depends` never inherits. `DONE` parents suppress subtrees.

## Alternatives

- Soft ordering (consider after, allow overlap): rejected — users cannot reason
  about "draft before review" that overlaps.
- Container duration summed and scheduled: rejected — double-books real work.
- Cycle auto-break by outline: rejected — silently violates an explicit order,
  against scheduling's never-silently-violate rule.
- SkedPal one-at-a-time sequential and gap fields: deferred to a later slice.

## Consequences

- Resolver materialises effective `Depends` (explicit + sequential chains,
  containers expanded to leaves) and reports `unknown-dependency` /
  `dependency-cycle` in `check`; planner enforces temporal gating and reports
  `blocked by`, `dependency cycle`, `unknown dependency`, `pin precedes
  dependency` in `forecast`/`schedule`.
- `Snapshot` gains `depends` + `order`, so dependency/structure edits release
  stood-down Blocks.
- Guides gain `guides/entering-tasks.md` (file-graph template, `((`
  autocomplete, `Tab` nesting, `check` loop).
