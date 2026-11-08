// Package parse reads the values Lodo stores in the Logseq graph: durations,
// dates, org timestamps, priorities, slugs, and Time Map window lines.
//
// Malformed input is always an error; the parser never guesses. Callers
// (especially internal/resolve) report Issues instead of reimplementing these
// rules.
package parse
