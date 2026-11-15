package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/resolve"
)

func testData() Data {
	loc := time.UTC
	taskBlock := &logseq.Block{
		UUID:      "task-1",
		Content:   "TODO Write the note\nduration:: 30m",
		Marker:    "TODO",
		PropsText: map[string]any{"duration": "30m"},
	}
	mapBlock := &logseq.Block{
		UUID:       "map-1",
		Content:    "Deep Work",
		Properties: map[string]any{"default": true},
		Children:   []*logseq.Block{{UUID: "w-1", Content: "Mon-Fri 09:00-12:00"}},
	}
	input := resolve.Input{
		Pages:        []*logseq.Page{{Name: "2026-01-01"}, {Name: "Time Maps"}},
		Trees:        map[string][]*logseq.Block{"2026-01-01": {taskBlock}, "Time Maps": {mapBlock}},
		TimeMapsPage: "Time Maps",
		Location:     loc,
	}
	return Data{
		Blocks:       map[string]*logseq.Block{"task-1": taskBlock},
		PageOf:       map[string]string{"task-1": "2026-01-01"},
		GraphOf:      map[string]string{"task-1": ""},
		Inputs:       map[string]resolve.Input{"": input},
		Location:     loc,
		TimeMapsPage: "Time Maps",
	}
}

func TestValidateEditedKeepsValid(t *testing.T) {
	d := testData()
	task, issues, ok := ValidateEdited(&d, "task-1", "TODO Write the note edited\nduration:: 45m")
	if !ok {
		t.Fatalf("valid edit blocked: %+v", issues)
	}
	if task.Duration != 45*time.Minute {
		t.Fatalf("duration = %v, want 45m", task.Duration)
	}
	if task.Text != "Write the note edited" {
		t.Fatalf("text = %q", task.Text)
	}
}

func TestValidateEditedRejectsBadDuration(t *testing.T) {
	d := testData()
	_, issues, ok := ValidateEdited(&d, "task-1", "TODO Write the note\nduration:: banana")
	if ok {
		t.Fatal("banana duration must not validate")
	}
	found := false
	for _, is := range issues {
		if is.Kind == resolve.IssueBadProperty {
			found = true
		}
	}
	if !found {
		t.Fatalf("want bad-property-value, got %+v", issues)
	}
}

func TestValidateEditedRejectsUnknownTimeMapThenPatchFixes(t *testing.T) {
	d := testData()
	_, issues, ok := ValidateEdited(&d, "task-1", "TODO Write the note\nduration:: 30m\ntime-map:: nope")
	if ok {
		t.Fatal("unknown time-map must not validate")
	}
	found := false
	for _, is := range issues {
		if is.Kind == resolve.IssueUnknownTimeMap {
			found = true
		}
	}
	if !found {
		t.Fatalf("want unknown-time-map, got %+v", issues)
	}
	patched := PatchTimeMap("TODO Write the note\nduration:: 30m\ntime-map:: nope", "deep-work", false)
	if strings.Contains(patched, "nope") {
		t.Fatalf("patch must replace slug:\n%s", patched)
	}
	d2 := testData()
	_, _, ok = ValidateEdited(&d2, "task-1", patched)
	if !ok {
		t.Fatalf("patched to deep-work should validate (maps resolved from graph): %+v", issues)
	}
}

func TestHighlightPreservesRaw(t *testing.T) {
	in := "TODO **Write** the note\nduration:: 30m\n[[Link]] ((12345678-1234-1234-1234-123456789012))"
	out := Highlight(in)
	if !strings.Contains(out, "**") {
		t.Fatalf("bold markers hidden:\n%s", out)
	}
	if !strings.Contains(out, "::") {
		t.Fatalf("prop separator hidden:\n%s", out)
	}
	if !strings.Contains(out, "duration") {
		t.Fatalf("prop key lost:\n%s", out)
	}
}
