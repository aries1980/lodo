// Package norm holds small normalizations shared across packages: ID
// matching, block keys, slot arithmetic and read-calendar lists.
//
// It is a leaf package on purpose so model, config, calendar, planner,
// resolve and run can all use it without import cycles.
package norm

import (
	"strconv"
	"strings"
	"time"
)

// ID normalizes a Task UUID, dependency ref or page name for comparison:
// Logseq names and UUIDs compare case-insensitively with surrounding space
// ignored.
func ID(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// BlockKey identifies one Block of one Task for in-memory maps. The NUL
// separator cannot appear in a UUID, so the encoding is unambiguous.
func BlockKey(taskID string, index int) string {
	return taskID + "\x00" + strconv.Itoa(index)
}

// Units converts a duration to whole granularity slots, rounding up.
// Non-positive durations need no slot.
func Units(d, granularity time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int((d + granularity - 1) / granularity)
}

// Phrases trims entries, compares them case-insensitively, drops empties,
// and drops duplicates, preserving first-seen order. Nil in stays nil out
// when nothing survives.
func Phrases(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		p := strings.ToLower(strings.TrimSpace(s))
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// ReadCalendars trims entries, drops empties, silently drops any read entry
// equal to the write calendar ID, and drops exact duplicates within the
// read list, preserving config order. Nil in stays nil out when nothing
// survives, so an absent list and an emptied one compare equal.
func ReadCalendars(writeID string, reads []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range reads {
		id := strings.TrimSpace(r)
		if id == "" || id == writeID {
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
