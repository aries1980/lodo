// Package parse provides parsing/normalization primitives for Logseq-authored
// values consumed by resolution and domain logic (durations, org timestamps and
// civil dates, priorities, time-map windows/slugs, and related helpers).
//
// It owns these parsing rules so callers (especially internal/resolve) can report
// Issues for unparseable input instead of reimplementing or guessing semantics.
package parse
