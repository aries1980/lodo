package keep

import (
	"errors"
	"fmt"
	"strings"

	"github.com/aries1980/lodo/internal/norm"
)

// ErrNotFound is returned when no note carries a requested title.
var ErrNotFound = errors.New("keep: note not found")

// FindByTitle locates a note by title, case-insensitively like page
// names. Duplicates are an error: two notes with one title cannot page a
// source deterministically.
func FindByTitle(notes []Note, title string) (Note, error) {
	want := norm.ID(title)
	var found *Note
	dups := 0
	for i := range notes {
		if norm.ID(notes[i].Title) == want {
			dups++
			cp := notes[i]
			found = &cp
		}
	}
	if found == nil {
		return Note{}, ErrNotFound
	}
	if dups > 1 {
		return Note{}, fmt.Errorf("keep: %d notes titled %q; titles must be unique within a source", dups, title)
	}
	return *found, nil
}

// SelectNotes applies the source filter to already-fetched notes:
// label matches (by id or name, case-insensitively). Trashed/archived
// filtering happens at fetch time per transport.
func SelectNotes(all []Note, label, labelID string) []Note {
	if label == "" {
		return all
	}
	var out []Note
	for _, n := range all {
		if HasLabel(n, label, labelID) {
			out = append(out, n)
		}
	}
	return out
}

// HasLabel reports whether the note carries the label by id or name.
func HasLabel(n Note, label, labelID string) bool {
	want := norm.ID(label)
	for _, l := range n.Labels {
		if labelID != "" && (l == labelID || lastSegment(l) == lastSegment(labelID)) {
			return true
		}
		if norm.ID(l) == want || norm.ID(lastSegment(l)) == want {
			return true
		}
	}
	return false
}

func lastSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}
