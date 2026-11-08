// Package resolve turns raw Logseq graph payloads into planner inputs: Tasks,
// Time Maps, validation Issues, and provenance (ADR 0006).
//
// Resolution is deterministic for a given Input and must not guess when graph
// data is missing, malformed, or ambiguous; unparseable or contradictory cases
// become reported Issues. Parsing primitives owned by internal/parse (durations,
// org stamps/civil dates, priorities, windows, slugification) are consumed here
// rather than reimplemented.
package resolve
