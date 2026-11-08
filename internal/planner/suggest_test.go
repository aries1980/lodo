package planner

import (
	"strings"
	"testing"
	"time"

	"github.com/aries1980/lodo/internal/model"
)

func makeSuggestFixture(tasks []model.Task, maps []model.TimeMap, report Report) (Input, Schedule) {
	loc := london
	in := Input{
		Tasks: tasks, TimeMaps: maps,
		Now:         at(9, 0, 0),
		Granularity: 15 * time.Minute,
		WindowDays:  7,
		Location:    loc,
	}
	schedule := Schedule{Report: report}
	return in, schedule
}

func hasReason(suggestions []Suggestion, reason string) bool {
	for _, s := range suggestions {
		if s.Reason == reason {
			return true
		}
	}
	return false
}

func TestSuggestExtendAndReduce(t *testing.T) {
	task := model.Task{ID: "t1", Text: "Write report", Marker: model.MarkerTODO, Duration: duration(240), TimeMaps: []string{"deep"}}
	end := civil(13)
	task.PlanEnd = &end
	maps := []model.TimeMap{deepMap(on(time.Monday, 9, 0, 10, 0))}
	report := Report{Tasks: []TaskReport{{TaskID: "t1", Required: 16, Placed: 4, Shortfall: 12, Reason: ReasonPartialFit}}}
	in, schedule := makeSuggestFixture([]model.Task{task}, maps, report)
	got := Suggest(in, schedule, nil)
	if !hasReason(got, SuggestExtendPlanEnd) {
		t.Fatalf("want extend-plan-end, got %+v", got)
	}
	if !hasReason(got, SuggestReduceDuration) {
		t.Fatalf("want reduce-duration, got %+v", got)
	}
	if !hasReason(got, SuggestAddTimeMapHours) {
		t.Fatalf("want add-time-map-hours, got %+v", got)
	}
}

func TestSuggestBlockedOnlyFixesBlocker(t *testing.T) {
	a := model.Task{ID: "a", Text: "A", Marker: model.MarkerTODO, Duration: duration(240)}
	b := model.Task{ID: "b", Text: "B", Marker: model.MarkerTODO, Duration: duration(60), Depends: []string{"a"}}
	maps := []model.TimeMap{deepMap(on(time.Monday, 9, 0, 10, 0))}
	report := Report{Tasks: []TaskReport{
		{TaskID: "a", Required: 16, Placed: 4, Shortfall: 12, Reason: ReasonPartialFit},
		{TaskID: "b", Required: 4, Placed: 0, Shortfall: 4, Reason: ReasonBlockedByDependency, BlockedBy: []string{"a"}},
	}}
	in, schedule := makeSuggestFixture([]model.Task{a, b}, maps, report)
	got := Suggest(in, schedule, nil)
	var forB []Suggestion
	for _, s := range got {
		if s.TaskID == "b" {
			forB = append(forB, s)
		}
	}
	if len(forB) == 0 || forB[0].Reason != SuggestCompleteDependency {
		t.Fatalf("B wants complete-dependency only, got %+v", forB)
	}
	for _, s := range forB {
		if s.Reason == SuggestExtendPlanEnd {
			t.Fatalf("blocked task must not suggest date move, got %+v", forB)
		}
	}
}

func TestSuggestPinGetsNothing(t *testing.T) {
	pin := at(10, 9, 30)
	task := model.Task{ID: "p", Text: "Pinned", Marker: model.MarkerTODO, Duration: duration(30), Pin: &pin, Depends: []string{"a"}}
	report := Report{Tasks: []TaskReport{{TaskID: "p", Shortfall: 0, Reason: ReasonPinPrecedesDependency, BlockedBy: []string{"a"}}}}
	in, schedule := makeSuggestFixture([]model.Task{task}, nil, report)
	if got := Suggest(in, schedule, nil); len(got) != 0 {
		t.Fatalf("pin violation wants no suggestions, got %+v", got)
	}
}

func TestSuggestUnknownMap(t *testing.T) {
	task := model.Task{ID: "t1", Text: "T", Marker: model.MarkerTODO, Duration: duration(60), TimeMaps: []string{"nope"}}
	maps := []model.TimeMap{deepMap(on(time.Monday, 9, 0, 10, 0))}
	report := Report{Tasks: []TaskReport{{TaskID: "t1", Required: 4, Placed: 0, Shortfall: 4, Reason: ReasonNoEligibleTime}}}
	in, schedule := makeSuggestFixture([]model.Task{task}, maps, report)
	got := Suggest(in, schedule, nil)
	if !hasReason(got, SuggestFixTimeMap) {
		t.Fatalf("want fix-time-map, got %+v", got)
	}
	if hasReason(got, SuggestExtendPlanEnd) {
		t.Fatalf("unknown map must not suggest date move, got %+v", got)
	}
}

func TestSuggestOverdueAvoidsExtend(t *testing.T) {
	task := model.Task{ID: "t1", Text: "Overdue", Marker: model.MarkerTODO, Duration: duration(240)}
	yesterday := model.CivilDateOf(at(9, 0, 0).AddDate(0, 0, -1))
	task.PlanEnd = &yesterday
	maps := []model.TimeMap{deepMap(on(time.Monday, 9, 0, 10, 0))}
	report := Report{Tasks: []TaskReport{{TaskID: "t1", Required: 16, Placed: 4, Shortfall: 12, Reason: ReasonPartialFit}}}
	in, schedule := makeSuggestFixture([]model.Task{task}, maps, report)
	got := Suggest(in, schedule, nil)
	if hasReason(got, SuggestExtendPlanEnd) {
		t.Fatalf("overdue must not suggest extend-plan-end, got %+v", got)
	}
	if !hasReason(got, SuggestAddTimeMapHours) || !hasReason(got, SuggestReduceDuration) {
		t.Fatalf("overdue wants hours/duration, got %+v", got)
	}
}

func TestSuggestFlexibleSkipsExtend(t *testing.T) {
	task := model.Task{ID: "t1", Text: "Flex", Marker: model.MarkerTODO, Duration: duration(240), TimeMaps: []string{"deep"}}
	maps := []model.TimeMap{deepMap(on(time.Monday, 9, 0, 10, 0))}
	report := Report{Tasks: []TaskReport{{TaskID: "t1", Required: 16, Placed: 4, Shortfall: 12, Reason: ReasonPartialFit}}}
	in, schedule := makeSuggestFixture([]model.Task{task}, maps, report)
	got := Suggest(in, schedule, nil)
	if hasReason(got, SuggestExtendPlanEnd) {
		t.Fatalf("flexible task must not suggest extend-plan-end, got %+v", got)
	}
	if !hasReason(got, SuggestAddTimeMapHours) || !hasReason(got, SuggestReduceDuration) {
		t.Fatalf("flexible wants hours/duration, got %+v", got)
	}
}

func TestSuggestSpillRespectsTiers(t *testing.T) {
	mkTask := func(id string, maps ...string) model.Task {
		end := civil(13)
		return model.Task{ID: id, Text: id, Marker: model.MarkerTODO, Duration: duration(240), TimeMaps: maps, PlanEnd: &end}
	}
	yellowProj := model.TimeMap{Slug: "proj", Windows: []model.Window{onTier(time.Monday, 9, 0, 10, 0, model.TierYellow)}}
	greenDef := model.TimeMap{Slug: "deep", Default: true, Windows: []model.Window{on(time.Monday, 9, 0, 10, 0)}}
	redDef := model.TimeMap{Slug: "deep", Default: true, Windows: []model.Window{onTier(time.Monday, 9, 0, 10, 0, model.TierRed)}}
	yellowDef := model.TimeMap{Slug: "deep", Default: true, Windows: []model.Window{onTier(time.Monday, 9, 0, 10, 0, model.TierYellow)}}
	redProj := model.TimeMap{Slug: "proj", Windows: []model.Window{onTier(time.Monday, 9, 0, 10, 0, model.TierRed)}}

	// Yellow-only task with green-only default: no spill (never upward).
	task := mkTask("t1", "proj")
	report := Report{Tasks: []TaskReport{{TaskID: "t1", Required: 4, Placed: 0, Shortfall: 4, Reason: ReasonPartialFit}}}
	in, schedule := makeSuggestFixture([]model.Task{task}, []model.TimeMap{yellowProj, greenDef}, report)
	got := Suggest(in, schedule, nil)
	for _, s := range got {
		if s.TaskID == "t1" && (s.Reason == SuggestSpillToDefaultYellow || s.Reason == SuggestSpillToDefaultGreen) {
			t.Fatalf("yellow task must never spill to green, got %+v", got)
		}
	}

	// Red task with yellow-only default: no yellow spill.
	rtask := mkTask("r1", "proj")
	rreport := Report{Tasks: []TaskReport{{TaskID: "r1", Required: 4, Placed: 0, Shortfall: 4, Reason: ReasonPartialFit}}}
	in2, sched2 := makeSuggestFixture([]model.Task{rtask}, []model.TimeMap{redProj, yellowDef}, rreport)
	got2 := Suggest(in2, sched2, nil)
	for _, s := range got2 {
		if s.TaskID == "r1" && s.Reason == SuggestSpillToDefaultYellow {
			t.Fatalf("red task must never spill to yellow, got %+v", got2)
		}
	}

	// Red task with red default: red spill allowed.
	in3, sched3 := makeSuggestFixture([]model.Task{rtask}, []model.TimeMap{redProj, redDef}, rreport)
	got3 := Suggest(in3, sched3, nil)
	if !hasReason(got3, SuggestSpillToDefaultRed) {
		t.Fatalf("red task with red default wants red spill, got %+v", got3)
	}
	for _, s := range got3 {
		if s.Reason == SuggestSpillToDefaultRed && !strings.Contains(s.Impact, "if free") {
			t.Fatalf("spill impact must hedge on free capacity, got %q", s.Impact)
		}
	}
}

func TestSuggestDownstreamOfCycleSuggestsFixCycle(t *testing.T) {
	a := model.Task{ID: "a", Text: "A", Marker: model.MarkerTODO, Duration: duration(60), Depends: []string{"b"}}
	b := model.Task{ID: "b", Text: "B", Marker: model.MarkerTODO, Duration: duration(60), Depends: []string{"a"}}
	c := model.Task{ID: "c", Text: "C", Marker: model.MarkerTODO, Duration: duration(60), Depends: []string{"a"}}
	maps := []model.TimeMap{deepMap(on(time.Monday, 9, 0, 10, 0))}
	report := Report{Tasks: []TaskReport{
		{TaskID: "c", Required: 4, Placed: 0, Shortfall: 4, Reason: ReasonBlockedByDependency, BlockedBy: []string{"a"}},
	}}
	in, schedule := makeSuggestFixture([]model.Task{a, b, c}, maps, report)
	cycles := map[string]bool{"a": true, "b": true}
	got := Suggest(in, schedule, cycles)
	found := false
	for _, s := range got {
		if s.TaskID == "c" && s.Reason == SuggestFixCycle {
			found = true
		}
		if s.TaskID == "c" && s.Reason == SuggestExtendPlanEnd {
			t.Fatalf("downstream of cycle must not suggest date move, got %+v", got)
		}
	}
	if !found {
		t.Fatalf("downstream of cycle wants fix-cycle, got %+v", got)
	}
}

func TestSuggestStoodDownSuggestsRestore(t *testing.T) {
	task := model.Task{ID: "t1", Text: "T", Marker: model.MarkerTODO, Duration: duration(60)}
	end := civil(13)
	task.PlanEnd = &end
	maps := []model.TimeMap{deepMap(on(time.Monday, 9, 0, 10, 0))}
	report := Report{Tasks: []TaskReport{{TaskID: "t1", Required: 8, Placed: 4, StoodDown: 2, Shortfall: 2, Reason: ReasonPartialFit}}}
	in, schedule := makeSuggestFixture([]model.Task{task}, maps, report)
	got := Suggest(in, schedule, nil)
	if !hasReason(got, SuggestRestoreStoodDown) {
		t.Fatalf("stood-down wants restore suggestion, got %+v", got)
	}
}

func TestSuggestHoursNamesDays(t *testing.T) {
	task := model.Task{ID: "t1", Text: "T", Marker: model.MarkerTODO, Duration: duration(240), TimeMaps: []string{"deep"}}
	end := civil(13)
	task.PlanEnd = &end
	maps := []model.TimeMap{deepMap(on(time.Monday, 9, 0, 10, 0))}
	report := Report{Tasks: []TaskReport{{TaskID: "t1", Required: 16, Placed: 4, Shortfall: 12, Reason: ReasonPartialFit}}}
	in, schedule := makeSuggestFixture([]model.Task{task}, maps, report)
	got := Suggest(in, schedule, nil)
	for _, s := range got {
		if s.Reason == SuggestAddTimeMapHours {
			if s.Change == "add Time Map hours on free days" {
				continue
			}
			return
		}
	}
	t.Fatalf("hours suggestion should name specific days when possible, got %+v", got)
}

func TestSuggestExtendSuppressedBeyondHorizon(t *testing.T) {
	task := model.Task{ID: "t1", Text: "Write report", Marker: model.MarkerTODO, Duration: duration(240), TimeMaps: []string{"deep"}}
	far := civil(20)
	task.PlanEnd = &far
	maps := []model.TimeMap{deepMap(on(time.Monday, 9, 0, 10, 0))}
	report := Report{Tasks: []TaskReport{{TaskID: "t1", Required: 16, Placed: 4, Shortfall: 12, Reason: ReasonPartialFit}}}
	in, schedule := makeSuggestFixture([]model.Task{task}, maps, report)
	got := Suggest(in, schedule, nil)
	if hasReason(got, SuggestExtendPlanEnd) {
		t.Fatalf("plan-end beyond horizon must not suggest extend-plan-end, got %+v", got)
	}
	if !hasReason(got, SuggestReduceDuration) || !hasReason(got, SuggestAddTimeMapHours) {
		t.Fatalf("beyond-horizon still wants hours/duration, got %+v", got)
	}
}

func TestSuggestExtendImpactIsEstimate(t *testing.T) {
	task := model.Task{ID: "t1", Text: "Write report", Marker: model.MarkerTODO, Duration: duration(240), TimeMaps: []string{"deep"}}
	end := civil(13)
	task.PlanEnd = &end
	maps := []model.TimeMap{deepMap(on(time.Monday, 9, 0, 10, 0))}
	report := Report{Tasks: []TaskReport{{TaskID: "t1", Required: 16, Placed: 4, Shortfall: 12, Reason: ReasonPartialFit}}}
	in, schedule := makeSuggestFixture([]model.Task{task}, maps, report)
	got := Suggest(in, schedule, nil)
	for _, s := range got {
		if s.Reason == SuggestExtendPlanEnd {
			if s.Impact == "" || len(s.Impact) < 5 || s.Impact[:4] != "adds" {
				t.Fatalf("extend impact must be qualified as an estimate, got %q", s.Impact)
			}
			return
		}
	}
	t.Fatalf("want extend-plan-end, got %+v", got)
}

func TestSuggestAddHoursFiltersFallbackTier(t *testing.T) {
	task := model.Task{ID: "t1", Text: "T", Marker: model.MarkerTODO, Duration: duration(240), TimeMaps: []string{"proj"}}
	end := civil(13)
	task.PlanEnd = &end
	yellowProj := model.TimeMap{Slug: "proj", Windows: []model.Window{onTier(time.Monday, 9, 0, 10, 0, model.TierYellow)}}
	greenDef := model.TimeMap{Slug: "deep", Default: true, Windows: []model.Window{
		onTier(time.Monday, 9, 0, 10, 0, model.TierGreen),
		onTier(time.Tuesday, 9, 0, 10, 0, model.TierGreen),
		onTier(time.Wednesday, 9, 0, 10, 0, model.TierGreen),
		onTier(time.Thursday, 9, 0, 10, 0, model.TierGreen),
		onTier(time.Friday, 9, 0, 10, 0, model.TierGreen),
		onTier(time.Saturday, 9, 0, 10, 0, model.TierGreen),
		onTier(time.Sunday, 9, 0, 10, 0, model.TierGreen),
	}}
	maps := []model.TimeMap{yellowProj, greenDef}
	report := Report{Tasks: []TaskReport{{TaskID: "t1", Required: 4, Placed: 0, Shortfall: 4, Reason: ReasonPartialFit}}}
	in, schedule := makeSuggestFixture([]model.Task{task}, maps, report)
	got := Suggest(in, schedule, nil)
	for _, s := range got {
		if s.Reason == SuggestAddTimeMapHours {
			if containsDay(s.Change, "Mon") {
				t.Fatalf("yellow task hours must ignore upward green fallback days, got %q (all %+v)", s.Change, got)
			}
			return
		}
	}
	t.Fatalf("want add-time-map-hours, got %+v", got)
}

func TestSuggestStoodDownSuppressedWhenBlocked(t *testing.T) {
	a := model.Task{ID: "a", Text: "A", Marker: model.MarkerTODO, Duration: duration(240)}
	b := model.Task{ID: "b", Text: "B", Marker: model.MarkerTODO, Duration: duration(60), Depends: []string{"a"}}
	maps := []model.TimeMap{deepMap(on(time.Monday, 9, 0, 10, 0))}
	report := Report{Tasks: []TaskReport{
		{TaskID: "b", Required: 4, Placed: 0, StoodDown: 2, Shortfall: 4, Reason: ReasonBlockedByDependency, BlockedBy: []string{"a"}},
	}}
	in, schedule := makeSuggestFixture([]model.Task{a, b}, maps, report)
	got := Suggest(in, schedule, nil)
	for _, s := range got {
		if s.TaskID == "b" && s.Reason == SuggestRestoreStoodDown {
			t.Fatalf("blocked task must not suggest restore-stood-down, got %+v", got)
		}
	}
	if !hasReason(got, SuggestCompleteDependency) {
		t.Fatalf("blocked task still wants blocker fix, got %+v", got)
	}
}

func containsDay(change, day string) bool {
	for _, part := range splitDays(change) {
		if part == day {
			return true
		}
	}
	return false
}

func splitDays(change string) []string {
	start := -1
	for i := 0; i < len(change); i++ {
		if change[i] == 'n' && i+1 < len(change) && change[i+1] == ' ' {
			start = i + 2
			break
		}
	}
	if start < 0 || start >= len(change) {
		return nil
	}
	var out []string
	cur := ""
	for _, c := range change[start:] + "," {
		if c == ',' || c == ' ' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(c)
	}
	return out
}
