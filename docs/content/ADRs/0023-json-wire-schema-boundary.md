---
title: "One versioned wire schema for command output"
weight: 230
---

> Status: **accepted.**

The `--json` schema had grown two ways. `check`, `forecast`, `schedule`, and
`refresh` emitted a set of CLI-local DTOs with a mapper in `views.go`.
`explain` emitted the planner's tagged decision types directly plus its own
envelope. Only `explain` carried a `schema_version`, and the ambiguous-query
payload used `map[string]string`. No single place answered "what does each
command emit?".

`internal/wire` is now that place. It declares every result payload, every
shared row type, and every schema-version constant. `internal/cli/wire.go` maps
run outcomes onto those types (it needs the `task` and `graph` display joins,
which are CLI concerns) and owns the one stdout serializer. No other package
declares a command payload, and only `internal/cli` imports the package.

Every payload starts with `schema_version`. Versions are per command: `check`,
`forecast` (and `schedule --dry-run`), `schedule`, `refresh`, and `auth google`
start at `1`; `explain` keeps `2`, which it had before this record. A version
changes additively: fields may be added, never removed, renamed, or retyped,
and readers ignore unknown fields. A shape change bumps the version even when
it is additive, so a consumer can notice. A payload without `schema_version` is
the unversioned pre-1 shape (version `0`).

`explain` is the one deliberate exception. Its per-task `eligibility`,
`ordering`, `dependency_gate`, `plan_window`, `time_maps`, `placement`,
`final_blocks`, and `outcome` are the planner's actual decision trace
(`internal/planner/trace.go`). Duplicating those ~95 fields into `internal/wire`
would add a permanent mapping and a silent-drift risk: a new trace fact would
be recorded by the planner and never reach the payload, which is precisely what
`explain` exists to prevent. The trace types are therefore part of the
versioned explain schema; every other domain type is converted at the boundary.
This is why `model.Disposition` never reaches a payload: its `Snapshot` carries
a `time.Duration`, which `encoding/json/v2` refuses to marshal, and its
untagged fields would leak Go casing.

Tagging the domain types and deleting the DTO layer was rejected. The summary
payloads need `task` and `graph` labels that `model.Block`, `planner.TaskReport`,
`Suggestion`, `Explanation`, `run.WriteOp`, `run.Orphan`, and `store.Binding` do
not have; adding them would put presentation text into the planner and model.
A mapper would remain for those joins, so the DTO layer would not actually
disappear. Tagging also forces wire decisions onto internals such as
`Disposition.Snapshot` and `model.Task`'s planning-only fields.

A single global version was rejected: unrelated payloads would bump together
and tell a consumer nothing about which shape changed. Unversioned summary
payloads were rejected: a consumer could not detect a shape change, while the
field is additive and safe to introduce. Map-keyed output was rejected because
map key order is not a contract; structs keep the bytes stable run to run.

Consequences: a new result field is a three-file change (the `wire` type, the
mapping in `internal/cli/wire.go`, and `docs/content/specs/cli.md`) plus the
schema-version bump. Shared row types belong to several payloads, so changing
one bumps every payload that contains it. The stderr JSONL run records are
diagnostics, not this result schema, and stay out of scope.
