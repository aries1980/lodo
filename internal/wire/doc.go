// Package wire defines the JSON result payloads every lodo command emits with
// --json.
//
// These are wire types, not domain types: they carry the display joins (Task
// text and graph slug), string enums, and schema versions consumers depend on.
// internal/cli maps run outcomes onto them in internal/cli/wire.go; no other
// package declares a command payload, and internal/cli is the only importer.
//
// Version policy: every payload starts with schema_version. A version changes
// additively: fields may be added, never removed, renamed, or retyped. A shape
// change bumps the version even when it is additive, so a consumer can notice.
// A payload without schema_version is the unversioned pre-1 shape (version 0).
//
// The explain command is the one deliberate exception: its per-task
// eligibility, ordering, dependency gate, plan window, time maps, placement,
// final blocks, and outcome are the planner's actual decision trace
// (internal/planner/trace.go). Keeping those types is what stops the
// explanation drifting from the placement it describes; they are therefore
// part of the versioned explain schema.
package wire
