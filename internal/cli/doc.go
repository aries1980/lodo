// Package cli implements the `lodo` Cobra command tree, flag/config/credential
// override wiring, command handlers, and user-facing output.
//
// Run owns per-invocation option state and returns a process exit code; handlers
// should keep control flow explicit and avoid package-global mutable request
// state. Read-only commands (check/forecast/explain, schedule --dry-run) must not
// mutate the calendar or commit store state. schedule writes the calendar before
// committing the store (ADR 0007). Logging/formatting follows the resolved
// --log-format/--verbose/--disable-colors settings.
package cli
