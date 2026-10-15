package planner

import (
	"testing"
	"time"

	"github.com/aries1980/lodo/internal/model"
)

func depTask(id string, d time.Duration, depends ...string) model.Task {
	t := localTask(id, d)
	t.Depends = depends
	return t
}

func TestDependsFinishToStart(t *testing.T) {
	a := localTask("a", duration(60))
	b := depTask("b", duration(60), "a")
	in := newInput(a, b)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 12, 0))}
	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(s.Blocks) != 2 {
		t.Fatalf("Blocks=%v want 2", describe(s.Blocks))
	}
	if s.Blocks[0].TaskID != "a" || s.Blocks[1].TaskID != "b" {
		t.Fatalf("a must precede b: %+v", s.Blocks)
	}
	if !s.Blocks[1].Start.After(s.Blocks[0].End) && !s.Blocks[1].Start.Equal(s.Blocks[0].End) {
		t.Fatalf("b must start at/after a end: %+v", s.Blocks)
	}
}

func TestTopoOverridesBuckets(t *testing.T) {
	// b time-bound due Tue, a flexible; b depends on a -> a first.
	a := localTask("a", duration(60))
	b := depTask("b", duration(60), "a")
	end := civil(13) // Wed
	b.PlanEnd = &end
	in := newInput(b, a) // input order time-bound first
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 12, 0))}
	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(s.Blocks) != 2 || s.Blocks[0].TaskID != "a" {
		t.Fatalf("flexible dep must precede time-bound dependent: %v", describe(s.Blocks))
	}
}

func TestFullGateBlocksOnShortfall(t *testing.T) {
	// a needs 2h but only 30m available -> shortfall; b depends on a -> blocked.
	a := localTask("a", duration(120))
	b := depTask("b", duration(30), "a")
	in := newInput(a, b)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 9, 30))}
	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	found := false
	for _, r := range s.Report.Tasks {
		if r.TaskID == "b" && r.Reason == ReasonBlockedByDependency {
			found = true
		}
	}
	if !found {
		t.Fatalf("b must be blocked by incomplete a: %+v", s.Report.Tasks)
	}
}

func TestCycleIsReported(t *testing.T) {
	a := depTask("a", duration(30), "b")
	b := depTask("b", duration(30), "a")
	in := newInput(a, b)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 12, 0))}
	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(s.Blocks) != 0 {
		t.Fatalf("cycle must place nothing: %+v", s.Blocks)
	}
	if len(s.Report.Tasks) != 2 {
		t.Fatalf("both in cycle reported: %+v", s.Report.Tasks)
	}
	for _, r := range s.Report.Tasks {
		if r.Reason != ReasonDependencyCycle {
			t.Fatalf("reason must be cycle: %+v", r)
		}
	}
}

func TestUnknownDependsBlocks(t *testing.T) {
	a := depTask("a", duration(30), "ghost-id")
	in := newInput(a)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 12, 0))}
	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(s.Blocks) != 0 || len(s.Report.Tasks) != 1 || s.Report.Tasks[0].Reason != ReasonUnknownDependency {
		t.Fatalf("unknown dep must block: %+v %+v", s.Blocks, s.Report.Tasks)
	}
}

func TestPinPrecedesDependencyReported(t *testing.T) {
	a := localTask("a", duration(60))
	b := depTask("b", duration(30), "a")
	pin := at(9, 9, 0) // Mon 09:00, before a can finish
	b.Pin = &pin
	in := newInput(a, b)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 12, 0))}
	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	// Pin stays.
	foundPin := false
	for _, blk := range s.Blocks {
		if blk.TaskID == "b" && blk.Kind == model.BlockPin {
			foundPin = true
		}
	}
	if !foundPin {
		t.Fatalf("pin must stay: %+v", s.Blocks)
	}
	found := false
	for _, r := range s.Report.Tasks {
		if r.TaskID == "b" && r.Reason == ReasonPinPrecedesDependency {
			found = true
		}
	}
	if !found {
		t.Fatalf("pin violation must be reported: %+v", s.Report.Tasks)
	}
}

func TestContainerSkipped(t *testing.T) {
	a := localTask("a", duration(60))
	a.Container = true
	b := localTask("b", duration(30))
	in := newInput(a, b)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 12, 0))}
	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	for _, blk := range s.Blocks {
		if blk.TaskID == "a" {
			t.Fatalf("container must not be scheduled: %+v", s.Blocks)
		}
	}
}

func TestStoodDownDepBlocks(t *testing.T) {
	a := localTask("a", duration(60))
	b := depTask("b", duration(30), "a")
	in := newInput(a, b)
	in.TimeMaps = []model.TimeMap{deepMap(on(time.Monday, 9, 0, 12, 0))}
	in.Dispositions = []model.Disposition{{TaskID: "a", BlockIndex: 0, Kind: model.DispositionStoodDown, Slots: 4}}
	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	found := false
	for _, r := range s.Report.Tasks {
		if r.TaskID == "b" && r.Reason == ReasonBlockedByDependency {
			found = true
		}
	}
	if !found {
		t.Fatalf("stood-down dep must block: %+v", s.Report.Tasks)
	}
}
