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

// ParseOrder parses the sequential/parallel flag for a Task's children,
// case-insensitive. Empty means parallel (the default).
func ParseOrder(s string) (model.OrderMode, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	switch v {
	case "", "parallel":
		return model.OrderParallel, nil
	case "sequential":
		return model.OrderSequential, nil
	default:
		return "", fmt.Errorf("parse order %q: want sequential or parallel", s)
	}
}

// ParseDepends parses a strict comma-separated list of Logseq block refs,
// e.g. "((uuid)), ((uuid))". Each ref must hold a valid UUID; surrounding
// space is trimmed. Duplicates collapse (first wins). Any non-ref piece is
// an error; the caller reports it and blocks the dependent rather than
// guessing.
func ParseDepends(s string) ([]string, error) {
	text := strings.TrimSpace(s)
	if text == "" {
		return nil, nil
	}
	var out []string
	seen := map[string]bool{}
	for _, piece := range strings.Split(text, ",") {
		p := strings.TrimSpace(piece)
		if p == "" {
			return nil, fmt.Errorf("parse depends %q: empty entry", s)
		}
		id, ok := blockRefUUID(p)
		if !ok {
			return nil, fmt.Errorf("parse depends %q: %q is not a ((uuid)) block reference", s, p)
		}
		lower := strings.ToLower(strings.TrimSpace(id))
		if !seen[lower] {
			seen[lower] = true
			out = append(out, lower)
		}
	}
	return out, nil
}

// blockRefUUID extracts the UUID from a ((uuid)) block reference, allowing
// surrounding space inside the parens.
func blockRefUUID(s string) (string, bool) {
	v := strings.TrimSpace(s)
	if !strings.HasPrefix(v, "((") || !strings.HasSuffix(v, "))") || len(v) < 6 {
		return "", false
	}
	inner := strings.TrimSpace(v[2 : len(v)-2])
	if inner == "" {
		return "", false
	}
	if !isUUID(inner) {
		return "", false
	}
	return inner, true
}

// isUUID reports whether s parses as a UUID (any version), case-insensitive.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if c != '-' {
				return false
			}
		case (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'):
		default:
			return false
		}
	}
	return true
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
