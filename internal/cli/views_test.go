package cli

import (
	"slices"
	"testing"

	"github.com/aries1980/lodo/internal/jsonx"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/planner"
	"github.com/aries1980/lodo/internal/run"
)

func TestMapViews(t *testing.T) {
	in := []int{3, 1, 2}
	var seen []int
	out := mapViews(in, func(n int) int {
		seen = append(seen, n)
		return n * 2
	})
	if !slices.Equal(out, []int{6, 2, 4}) || !slices.Equal(seen, in) {
		t.Fatalf("out = %v, calls = %v", out, seen)
	}
	out[0] = 0
	if in[0] != 3 {
		t.Fatal("mapped output aliases input")
	}
}

func TestEmptyViewsAreArrays(t *testing.T) {
	views := []any{
		blockViews(nil, nil), shortfallViews(nil, nil), suggestionViews(nil, nil),
		explanationViews(nil), dispositionViews(nil, nil), orphanViews(nil, nil),
		blockerLabels(nil, nil), managedEvents(run.ForecastOutcome{}), checkStoodDown(run.CheckOutcome{}),
	}
	for _, view := range views {
		raw, err := jsonx.Marshal(view)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != "[]" {
			t.Fatalf("%T = %s, want []", view, raw)
		}
	}
}

func TestTaskViewLabels(t *testing.T) {
	tasks := []model.Task{
		{ID: "duplicate", Text: "first text", Graph: "first graph"},
		{ID: "duplicate", Text: "last text", Graph: "last graph"},
	}
	out := blockViews(tasks, []model.Block{{TaskID: "duplicate"}, {TaskID: "missing"}})
	if out[0].Task != "last text" || out[0].Graph != "first graph" {
		t.Fatalf("duplicate label policy changed: %+v", out[0])
	}
	if out[1].Task != "" || out[1].Graph != "" || out[1].TaskID != "missing" {
		t.Fatalf("missing label policy changed: %+v", out[1])
	}
}

func TestShortfallViewsDoNotAliasBlockers(t *testing.T) {
	reports := []planner.TaskReport{{TaskID: "task", BlockedBy: []string{"dep"}}}
	out := shortfallViews(nil, reports)
	out[0].BlockedBy[0] = "changed"
	if reports[0].BlockedBy[0] != "dep" {
		t.Fatal("view aliases report blockers")
	}
}

func TestConsiderationOrder(t *testing.T) {
	in := []planner.TaskDecision{
		{TaskID: "unconsidered-z"},
		{TaskID: "second", Ordering: planner.Ordering{Position: 2}},
		{TaskID: "first-b", Ordering: planner.Ordering{Position: 1}},
		{TaskID: "unconsidered-a"},
		{TaskID: "first-a", Ordering: planner.Ordering{Position: 1}},
	}
	slices.SortFunc(in, compareConsiderationOrder)
	got := mapViews(in, func(d planner.TaskDecision) string { return d.TaskID })
	want := []string{"first-a", "first-b", "second", "unconsidered-a", "unconsidered-z"}
	if !slices.Equal(got, want) {
		t.Fatalf("consideration order = %v, want %v", got, want)
	}
}
