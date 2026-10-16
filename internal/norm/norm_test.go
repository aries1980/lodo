package norm

import (
	"testing"
	"time"
)

func TestID(t *testing.T) {
	if got := ID("  AbC-123 "); got != "abc-123" {
		t.Fatalf("ID = %q, want abc-123", got)
	}
}

func TestBlockKey(t *testing.T) {
	if got := BlockKey("task", 2); got != "task\x002" {
		t.Fatalf("BlockKey = %q", got)
	}
}

func TestUnits(t *testing.T) {
	if got := Units(90*time.Minute, 30*time.Minute); got != 3 {
		t.Fatalf("Units = %d, want 3", got)
	}
	if got := Units(31*time.Minute, 30*time.Minute); got != 2 {
		t.Fatalf("Units ceil = %d, want 2", got)
	}
	if got := Units(0, 15*time.Minute); got != 0 {
		t.Fatalf("Units zero = %d, want 0", got)
	}
}

func TestPhrases(t *testing.T) {
	got := Phrases([]string{" Birthday ", "", "birthday", "Holiday"})
	if len(got) != 2 || got[0] != "birthday" || got[1] != "holiday" {
		t.Fatalf("Phrases = %q", got)
	}
	if got := Phrases(nil); got != nil {
		t.Fatalf("Phrases nil = %q, want nil", got)
	}
}

func TestReadCalendars(t *testing.T) {
	got := ReadCalendars("primary", []string{" a ", "", "primary", "a", "b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("ReadCalendars = %q", got)
	}
	if got := ReadCalendars("primary", nil); got != nil {
		t.Fatalf("ReadCalendars nil = %q, want nil", got)
	}
}
