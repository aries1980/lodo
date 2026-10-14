package planner

import (
	"fmt"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/google/go-cmp/cmp"

	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/model"
)

var london = mustLocation("Europe/London")

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// The fixture week is Monday 2026-11-09, safely after the October DST
// transition, so the grid is plain 96-slot days.
func at(day, hour, min int) time.Time {
	return time.Date(2026, time.November, day, hour, min, 0, 0, london)
}

func civil(day int) model.CivilDate {
	return model.CivilDateOf(at(day, 0, 0))
}

func duration(minutes int) time.Duration {
	return time.Duration(minutes) * time.Minute
}

func on(day time.Weekday, fromHour, fromMin, toHour, toMin int) model.Window {
	return model.Window{
		Days:        []time.Weekday{day},
		StartMinute: fromHour*60 + fromMin,
		EndMinute:   toHour*60 + toMin,
		Tier:        model.TierGreen,
	}
}

func weekdays() []time.Weekday {
	return []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
}

func newInput(tasks ...model.Task) Input {
	return Input{
		Tasks:       tasks,
		Now:         at(9, 0, 0),
		Granularity: 15 * time.Minute,
		WindowDays:  7,
		Location:    london,
	}
}

func deepMap(windows ...model.Window) model.TimeMap {
	return model.TimeMap{Slug: "deep", Windows: windows}
}

func localTask(id string, d time.Duration) model.Task {
	return model.Task{ID: id, Text: id, Marker: model.MarkerTODO, Duration: d, TimeMaps: []string{"deep"}}
}

// describe renders Blocks as "Mon 09:00@30m" for compact assertions.
func describe(blocks []model.Block) []string {
	var out []string
	for _, b := range blocks {
		out = append(out, fmt.Sprintf("%s %s@%dm",
			b.Start.In(london).Format("Mon"), b.Start.In(london).Format("15:04"), b.Slots*15))
	}
	return out
}

// TestPlacementComparatorGolden is ADR 0010's worked-examples table.
func TestPlacementComparatorGolden(t *testing.T) {
	tests := []struct {
		name    string
		minutes int
		windows []model.Window
		want    []string
	}{
		{
			"fragments below the floor are never emitted",
			30,
			[]model.Window{on(time.Monday, 9, 0, 9, 15), on(time.Monday, 13, 0, 13, 15)},
			nil,
		},
		{
			"completeness: two blocks beat one larger block",
			60,
			[]model.Window{on(time.Monday, 9, 0, 9, 30), on(time.Monday, 13, 0, 13, 45)},
			[]string{"Mon 09:00@30m", "Mon 13:00@30m"},
		},
		{
			"the only placement that places all of it",
			75,
			[]model.Window{on(time.Monday, 9, 0, 9, 30), on(time.Monday, 13, 0, 13, 45)},
			[]string{"Mon 09:00@30m", "Mon 13:00@45m"},
		},
		{
			"same-day contiguity: one block beats two that start earlier",
			75,
			[]model.Window{on(time.Monday, 9, 0, 9, 30), on(time.Monday, 13, 0, 13, 45), on(time.Monday, 17, 0, 18, 15)},
			[]string{"Mon 17:00@75m"},
		},
		{
			"largest-first: the earliest run is filled as far as possible",
			90,
			[]model.Window{on(time.Monday, 9, 0, 10, 0), on(time.Monday, 13, 0, 14, 0)},
			[]string{"Mon 09:00@60m", "Mon 13:00@30m"},
		},
		{
			"front-loading across days: day one takes the fragment",
			75,
			[]model.Window{on(time.Monday, 9, 0, 9, 30), on(time.Tuesday, 9, 0, 10, 15)},
			[]string{"Mon 09:00@30m", "Tue 09:00@45m"},
		},
		{
			"a whole Task rounding to one unit is emitted as one unit",
			5,
			[]model.Window{on(time.Monday, 9, 0, 12, 0)},
			[]string{"Mon 09:00@15m"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := newInput(localTask("t1", duration(tt.minutes)))
			in.TimeMaps = []model.TimeMap{deepMap(tt.windows...)}

			s, err := Plan(in)
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if diff := cmp.Diff(tt.want, describe(s.Blocks)); diff != "" {
				t.Fatalf("Blocks mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestShortfallIsReported(t *testing.T) {
	in := newInput(localTask("t1", duration(30)))
	in.TimeMaps = []model.TimeMap{deepMap(
		on(time.Monday, 9, 0, 9, 15),
		on(time.Monday, 13, 0, 13, 15),
	)}

	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	want := []TaskReport{{
		TaskID:    "t1",
		Required:  2,
		Placed:    0,
		StoodDown: 0,
		Shortfall: 2,
		Reason:    ReasonNoEligibleTime,
	}}
	if diff := cmp.Diff(want, s.Report.Tasks); diff != "" {
		t.Fatalf("Report mismatch (-want +got):\n%s", diff)
	}
}

func TestTimeBoundBeatsFlexible(t *testing.T) {
	flexible := localTask("flex", duration(60))
	flexible.Priority = model.PriorityA

	bound := localTask("bound", duration(60))
	end := civil(10) // Tuesday
	bound.PlanEnd = &end

	in := newInput(flexible, bound)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 10, 0))}

	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	want := []model.Block{{
		TaskID: "bound", Index: 0, Start: at(9, 9, 0), End: at(9, 10, 0), Slots: 4, Kind: model.BlockFlexible,
	}}
	if diff := cmp.Diff(want, s.Blocks); diff != "" {
		t.Fatalf("Blocks mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]TaskReport{{
		TaskID: "flex", Required: 4, Placed: 0, Shortfall: 4, Reason: ReasonNoEligibleTime,
	}}, s.Report.Tasks); diff != "" {
		t.Fatalf("Report mismatch (-want +got):\n%s", diff)
	}
}

func TestPriorityOrdersFlexibleTasks(t *testing.T) {
	high := localTask("high", duration(30))
	high.Priority = model.PriorityB
	high.OutlineOrder = 1

	low := localTask("low", duration(30))
	low.Priority = model.PriorityC
	low.OutlineOrder = 0

	none := localTask("none", duration(30))
	none.OutlineOrder = 2

	in := newInput(low, none, high)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 9, 30))}

	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got := describe(s.Blocks); len(got) != 1 || got[0] != "Mon 09:00@30m" || s.Blocks[0].TaskID != "high" {
		t.Fatalf("priority B must win, got %+v", s.Blocks)
	}
	if diff := cmp.Diff([]TaskReport{
		{TaskID: "low", Required: 2, Shortfall: 2, Reason: ReasonNoEligibleTime},
		{TaskID: "none", Required: 2, Shortfall: 2, Reason: ReasonNoEligibleTime},
	}, s.Report.Tasks); diff != "" {
		t.Fatalf("Report mismatch (-want +got):\n%s", diff)
	}
}

func TestOutlineOrderBreaksTies(t *testing.T) {
	late := localTask("late", duration(30))
	late.OutlineOrder = 5
	early := localTask("early", duration(30))
	early.OutlineOrder = 1

	in := newInput(late, early)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 9, 30))}

	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(s.Blocks) != 1 || s.Blocks[0].TaskID != "early" {
		t.Fatalf("outline order must break the tie, got %+v", s.Blocks)
	}
}

func TestPlanEndOrdersTimeBoundTasks(t *testing.T) {
	later := localTask("later", duration(30))
	end := civil(12)
	later.PlanEnd = &end

	sooner := localTask("sooner", duration(30))
	soonerEnd := civil(11)
	sooner.PlanEnd = &soonerEnd

	in := newInput(later, sooner)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Tuesday, 9, 0, 9, 30))}

	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(s.Blocks) != 1 || s.Blocks[0].TaskID != "sooner" {
		t.Fatalf("the nearer plan-end must win, got %+v", s.Blocks)
	}
}

func TestOverdueIsScheduledASAP(t *testing.T) {
	task := localTask("overdue", duration(30))
	end := civil(8) // Sunday, before today
	task.PlanEnd = &end

	in := newInput(task)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 10, 0))}

	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got := describe(s.Blocks); len(got) != 1 || got[0] != "Mon 09:00@30m" {
		t.Fatalf("overdue Task must be scheduled ASAP, got %v", got)
	}
	if len(s.Report.Tasks) != 0 {
		t.Fatalf("overdue Task is not infeasible, got %+v", s.Report.Tasks)
	}
}

func TestBeyondHorizonIsSilent(t *testing.T) {
	task := localTask("future", duration(60))
	start := civil(16) // the first day past the 7-day window
	task.PlanStart = &start

	in := newInput(task)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 10, 0))}

	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(s.Blocks) != 0 || len(s.Report.Tasks) != 0 {
		t.Fatalf("a Task beyond the horizon must be left unplaced silently, got %+v", s)
	}
}

func TestPinOverridesEverything(t *testing.T) {
	pinned := localTask("pinned", duration(60))
	pin := at(9, 7, 30) // outside the Time Map, and in the past hour
	pinned.Pin = &pin

	other := localTask("other", duration(60))

	in := newInput(pinned, other)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 10, 0))}

	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	want := []model.Block{
		{TaskID: "pinned", Index: 0, Start: at(9, 7, 30), End: at(9, 8, 30), Slots: 4, Kind: model.BlockPin},
		{TaskID: "other", Index: 0, Start: at(9, 9, 0), End: at(9, 10, 0), Slots: 4, Kind: model.BlockFlexible},
	}
	if diff := cmp.Diff(want, s.Blocks); diff != "" {
		t.Fatalf("Blocks mismatch (-want +got):\n%s", diff)
	}
}

func TestPinnedBlockKeepsItsSpanAndReservesItsIndex(t *testing.T) {
	task := localTask("t1", duration(120))

	in := newInput(task)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 11, 0))}
	in.Dispositions = []model.Disposition{{
		TaskID: "t1", BlockIndex: 1, Kind: model.DispositionPinned,
		Start: at(9, 9, 30), End: at(9, 10, 0),
	}}

	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	want := []model.Block{
		{TaskID: "t1", Index: 0, Start: at(9, 9, 0), End: at(9, 9, 30), Slots: 2, Kind: model.BlockFlexible},
		{TaskID: "t1", Index: 1, Start: at(9, 9, 30), End: at(9, 10, 0), Slots: 2, Kind: model.BlockPinned},
		{TaskID: "t1", Index: 2, Start: at(9, 10, 0), End: at(9, 11, 0), Slots: 4, Kind: model.BlockFlexible},
	}
	if diff := cmp.Diff(want, s.Blocks); diff != "" {
		t.Fatalf("Blocks mismatch (-want +got):\n%s", diff)
	}
	if len(s.Report.Tasks) != 0 {
		t.Fatalf("Task is fully placed, got %+v", s.Report.Tasks)
	}
}

func TestStoodDownReducesRequired(t *testing.T) {
	task := localTask("t1", duration(120))

	in := newInput(task)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 11, 0))}
	in.Dispositions = []model.Disposition{{
		TaskID: "t1", BlockIndex: 2, Kind: model.DispositionStoodDown, Slots: 2,
	}}

	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	want := []model.Block{
		{TaskID: "t1", Index: 0, Start: at(9, 9, 0), End: at(9, 10, 30), Slots: 6, Kind: model.BlockFlexible},
	}
	if diff := cmp.Diff(want, s.Blocks); diff != "" {
		t.Fatalf("Blocks mismatch (-want +got):\n%s", diff)
	}
	if len(s.Report.Tasks) != 0 {
		t.Fatalf("stood-down hours are not a shortfall, got %+v", s.Report.Tasks)
	}
}

func TestBusyTimeBlocksWholeSlots(t *testing.T) {
	task := localTask("t1", duration(90))

	in := newInput(task)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 11, 0))}
	g := grid.Build(in.Now, in.Location, in.Granularity, in.WindowDays)
	in.Busy = g.Overlapping(at(9, 9, 30), at(9, 10, 0))

	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	want := []model.Block{
		{TaskID: "t1", Index: 0, Start: at(9, 9, 0), End: at(9, 9, 30), Slots: 2, Kind: model.BlockFlexible},
		{TaskID: "t1", Index: 1, Start: at(9, 10, 0), End: at(9, 11, 0), Slots: 4, Kind: model.BlockFlexible},
	}
	if diff := cmp.Diff(want, s.Blocks); diff != "" {
		t.Fatalf("Blocks mismatch (-want +got):\n%s", diff)
	}
}

func TestExplanationsDescribeTheRun(t *testing.T) {
	task := localTask("t1", duration(30))

	in := newInput(task)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 11, 0))}

	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	want := []Explanation{{
		TaskID: "t1", BlockIndex: 0, Kind: model.BlockFlexible,
		Start: at(9, 9, 0), Slots: 2, RunStart: at(9, 9, 0), RunSlots: 8,
	}}
	if diff := cmp.Diff(want, s.Explanations); diff != "" {
		t.Fatalf("Explanations mismatch (-want +got):\n%s", diff)
	}
}

func TestInputValidation(t *testing.T) {
	valid := func() Input {
		in := newInput(localTask("t1", duration(30)))
		in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 10, 0))}
		return in
	}

	tests := []struct {
		name string
		mut  func(*Input)
	}{
		{"nil location", func(in *Input) { in.Location = nil }},
		{"zero granularity", func(in *Input) { in.Granularity = 0 }},
		{"granularity over a day", func(in *Input) { in.Granularity = 25 * time.Hour }},
		{"zero window days", func(in *Input) { in.WindowDays = 0 }},
		{"wrong busy mask size", func(in *Input) { in.Busy = grid.NewMask(7) }},
		{"non-positive duration", func(in *Input) { in.Tasks[0].Duration = 0 }},
		{"empty pinned span", func(in *Input) {
			in.Dispositions = []model.Disposition{{TaskID: "t1", BlockIndex: 0, Kind: model.DispositionPinned}}
		}},
		{"negative stood-down size", func(in *Input) {
			in.Dispositions = []model.Disposition{{TaskID: "t1", BlockIndex: 0, Kind: model.DispositionStoodDown, Slots: -1}}
		}},
		{"negative block index", func(in *Input) {
			in.Dispositions = []model.Disposition{{TaskID: "t1", BlockIndex: -1, Kind: model.DispositionStoodDown}}
		}},
		{"unknown disposition kind", func(in *Input) {
			in.Dispositions = []model.Disposition{{TaskID: "t1", BlockIndex: 0, Kind: "teleported"}}
		}},
		{"duplicate disposition", func(in *Input) {
			in.Dispositions = []model.Disposition{
				{TaskID: "t1", BlockIndex: 0, Kind: model.DispositionStoodDown, Slots: 1},
				{TaskID: "t1", BlockIndex: 0, Kind: model.DispositionStoodDown, Slots: 1},
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := valid()
			tt.mut(&in)
			if _, err := Plan(in); err == nil {
				t.Fatal("Plan must reject malformed input")
			}
		})
	}
}
