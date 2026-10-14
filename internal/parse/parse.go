// Package parse reads the values Lodo stores in the Logseq graph: durations,
// dates, org timestamps, priorities, slugs and Time Map window lines.
//
// Malformed input is always an error; the parser never guesses.
package parse

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/aries1980/lodo/internal/model"
)

// ParseDuration parses a Task duration such as "90m", "2h" or "1h30m".
func ParseDuration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", s, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("parse duration %q: must be positive", s)
	}
	return d, nil
}

// ParsePriority parses a Logseq priority: A, B or C, case-insensitive.
func ParsePriority(s string) (model.Priority, error) {
	p := model.Priority(strings.ToUpper(strings.TrimSpace(s)))
	switch p {
	case model.PriorityA, model.PriorityB, model.PriorityC:
		return p, nil
	default:
		return "", fmt.Errorf("parse priority %q: want A, B or C", s)
	}
}

// ParseCivilDate parses a day-granular date such as "2026-10-20".
func ParseCivilDate(s string) (model.CivilDate, error) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	if err != nil {
		return model.CivilDate{}, fmt.Errorf("parse date %q: %w", s, err)
	}
	return model.CivilDateOf(t), nil
}

// Slugify converts a Time Map name to its key, case-insensitively:
// "Deep Work" becomes "deep-work".
func Slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
			continue
		}
		if b.Len() > 0 && !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}
