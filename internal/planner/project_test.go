package planner

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/aries1980/lodo/internal/model"
)

func projectMaps() (primary, def model.TimeMap) {
	primary = model.TimeMap{Slug: "jlp", Windows: []model.Window{
		on(time.Monday, 9, 0, 10, 0),
	}}
	def = model.TimeMap{Slug: "deep", Default: true, Windows: []model.Window{
		on(time.Monday, 13, 0, 14, 0),
	}}
	return primary, def
}

func TestProjectSpillsIntoDefault(t *testing.T) {
	primary, def := projectMaps()
	task := model.Task{ID: "t1", Text: "t1", Marker: model.MarkerTODO, Duration: duration(120), TimeMaps: []string{"jlp"}}
	in := newInput(task)
	in.TimeMaps = []model.TimeMap{primary, def}

	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	want := []string{"Mon 09:00@60m", "Mon 13:00@60m"}
	if diff := cmp.Diff(want, describe(s.Blocks)); diff != "" {
		t.Fatalf("Blocks mismatch (-want +got):\n%s", diff)
	}
	if len(s.Report.Tasks) != 0 {
		t.Fatalf("spilled task must not shortfall, got %+v", s.Report.Tasks)
	}
}

func TestProjectPrefersPrimaryGreen(t *testing.T) {
	primary, def := projectMaps()
	task := model.Task{ID: "t1", Text: "t1", Marker: model.MarkerTODO, Duration: duration(60), TimeMaps: []string{"jlp"}}
	in := newInput(task)
	in.TimeMaps = []model.TimeMap{primary, def}

	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	want := []string{"Mon 09:00@60m"}
	if diff := cmp.Diff(want, describe(s.Blocks)); diff != "" {
		t.Fatalf("Blocks mismatch (-want +got):\n%s", diff)
	}
}

func TestProjectTierFirstGreenDefaultBeatsYellowPrimary(t *testing.T) {
	primary := model.TimeMap{Slug: "jlp", Windows: []model.Window{
		onTier(time.Monday, 9, 0, 10, 0, model.TierGreen),
		onTier(time.Tuesday, 9, 0, 10, 0, model.TierYellow),
	}}
	def := model.TimeMap{Slug: "deep", Default: true, Windows: []model.Window{
		onTier(time.Monday, 13, 0, 14, 0, model.TierGreen),
	}}
	task := model.Task{ID: "t1", Text: "t1", Marker: model.MarkerTODO, Duration: duration(120), TimeMaps: []string{"jlp"}}
	in := newInput(task)
	in.TimeMaps = []model.TimeMap{primary, def}

	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	want := []string{"Mon 09:00@60m", "Mon 13:00@60m"}
	if diff := cmp.Diff(want, describe(s.Blocks)); diff != "" {
		t.Fatalf("Blocks mismatch (-want +got):\n%s", diff)
	}
}

func TestProjectNoSpillWithoutDefault(t *testing.T) {
	primary := model.TimeMap{Slug: "jlp", Windows: []model.Window{
		on(time.Monday, 9, 0, 10, 0),
	}}
	task := model.Task{ID: "t1", Text: "t1", Marker: model.MarkerTODO, Duration: duration(120), TimeMaps: []string{"jlp"}}
	in := newInput(task)
	in.TimeMaps = []model.TimeMap{primary}

	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	want := []string{"Mon 09:00@60m"}
	if diff := cmp.Diff(want, describe(s.Blocks)); diff != "" {
		t.Fatalf("Blocks mismatch (-want +got):\n%s", diff)
	}
	if len(s.Report.Tasks) != 1 || s.Report.Tasks[0].Shortfall != 4 {
		t.Fatalf("remaining hour must shortfall, got %+v", s.Report.Tasks)
	}
}

func TestProjectUnknownMapStaysStrict(t *testing.T) {
	def := model.TimeMap{Slug: "deep", Default: true, Windows: []model.Window{
		on(time.Monday, 13, 0, 14, 0),
	}}
	task := model.Task{ID: "t1", Text: "t1", Marker: model.MarkerTODO, Duration: duration(60), TimeMaps: []string{"nope"}}
	in := newInput(task)
	in.TimeMaps = []model.TimeMap{def}

	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(s.Blocks) != 0 {
		t.Fatalf("unknown-only map must not spill, got %v", describe(s.Blocks))
	}
}
