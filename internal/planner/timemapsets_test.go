package planner

import (
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/model"
)

func capacityTask(maps ...string) model.Task {
	return model.Task{ID: "t", Text: "t", Marker: model.MarkerTODO, Duration: 15 * time.Minute, TimeMaps: maps}
}

func TestTimeMapSetsSelection(t *testing.T) {
	t.Run("DefaultsAreSortedBySlug", func(t *testing.T) {
		maps := indexTimeMaps([]model.TimeMap{
			{Slug: "b-default", Default: true},
			{Slug: "a-default", Default: true},
			{Slug: "proj"},
		})
		got := newTimeMapSets(model.Task{}, maps)
		if diff := cmp.Diff([]string{"a-default", "b-default"}, mapSlugs(got.primary)); diff != "" {
			t.Fatalf("primary mismatch (-want +got):\n%s", diff)
		}
		if len(got.fallback) != 0 {
			t.Fatalf("defaults-only task must have no fallback, got %v", mapSlugs(got.fallback))
		}
	})

	t.Run("PrimaryIsDedupedAndFallbackExcludesIt", func(t *testing.T) {
		maps := indexTimeMaps([]model.TimeMap{
			{Slug: "proj", Default: true},
			{Slug: "other", Default: true},
		})
		got := newTimeMapSets(model.Task{TimeMaps: []string{"proj", "proj", "nope"}}, maps)
		if diff := cmp.Diff([]string{"proj"}, mapSlugs(got.primary)); diff != "" {
			t.Fatalf("primary mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"other"}, mapSlugs(got.fallback)); diff != "" {
			t.Fatalf("fallback mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("UnknownOnlyStaysStrict", func(t *testing.T) {
		maps := indexTimeMaps([]model.TimeMap{
			{Slug: "proj"},
			{Slug: "deep", Default: true},
		})
		got := newTimeMapSets(model.Task{TimeMaps: []string{"nope"}}, maps)
		if len(got.primary) != 0 || len(got.fallback) != 0 {
			t.Fatalf("unknown-only task must resolve to nothing, got primary=%v fallback=%v", mapSlugs(got.primary), mapSlugs(got.fallback))
		}
	})

	t.Run("EmptyNamedMapKeepsFallbackReachable", func(t *testing.T) {
		maps := indexTimeMaps([]model.TimeMap{
			{Slug: "proj"},
			{Slug: "deep", Default: true, Windows: []model.Window{on(time.Monday, 9, 0, 10, 0)}},
		})
		got := newTimeMapSets(model.Task{TimeMaps: []string{"proj"}}, maps)
		if diff := cmp.Diff([]string{"proj"}, mapSlugs(got.primary)); diff != "" {
			t.Fatalf("primary mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"deep"}, mapSlugs(got.fallback)); diff != "" {
			t.Fatalf("fallback mismatch (-want +got):\n%s", diff)
		}
		if got.best != 0 {
			t.Fatalf("empty map must rank green, got rank %d", got.best)
		}
	})
}

// TestTimeMapSetsTierDeclaration pins the one authored change that withdraws
// eligible hours: a yellow window added to an empty primary map declares the
// Task yellow, so the green fallback stops counting (ADR 0014).
func TestTimeMapSetsTierDeclaration(t *testing.T) {
	g := grid.Build(at(9, 0, 0), london, 15*time.Minute, weeklyDays)
	greenDefault := model.TimeMap{Slug: "deep", Default: true, Windows: []model.Window{on(time.Monday, 13, 0, 14, 0)}}
	task := model.Task{TimeMaps: []string{"proj"}}

	before := newTimeMapSets(task, indexTimeMaps([]model.TimeMap{
		{Slug: "proj"},
		greenDefault,
	}))
	after := newTimeMapSets(task, indexTimeMaps([]model.TimeMap{
		{Slug: "proj", Windows: []model.Window{onTier(time.Monday, 9, 0, 9, 15, model.TierYellow)}},
		greenDefault,
	}))
	if after.eligibleSlots(g) >= before.eligibleSlots(g) {
		t.Fatalf("declaring yellow on an empty map must withdraw the green fallback, got %d -> %d", before.eligibleSlots(g), after.eligibleSlots(g))
	}
}

// TestTimeMapCapacityZeroIffNoPlaceableSlot pins the shared eligibility model:
// the weekly slot estimate is zero exactly when placement finds no slot for the
// same Task and Time Maps. wantSlots also pins the arithmetic, so the two sides
// agreeing on zero cannot hide a wrong count.
func TestTimeMapCapacityZeroIffNoPlaceableSlot(t *testing.T) {
	dstNow := time.Date(2027, time.March, 28, 0, 0, 0, 0, london)
	defaultMap := func(windows ...model.Window) model.TimeMap {
		return model.TimeMap{Slug: "deep", Default: true, Windows: windows}
	}
	primaryMap := func(windows ...model.Window) model.TimeMap {
		return model.TimeMap{Slug: "proj", Windows: windows}
	}

	tests := []struct {
		name      string
		now       time.Time
		taskMaps  []string
		maps      []model.TimeMap
		wantSlots int
	}{
		{name: "no maps", wantSlots: 0},
		{name: "no windows", maps: []model.TimeMap{defaultMap()}, wantSlots: 0},
		{name: "window without days", maps: []model.TimeMap{defaultMap(model.Window{StartMinute: 9 * 60, EndMinute: 10 * 60})}, wantSlots: 0},
		{name: "unknown only", taskMaps: []string{"nope"}, maps: []model.TimeMap{defaultMap(on(time.Monday, 13, 0, 14, 0))}, wantSlots: 0},
		{name: "default weekdays", maps: []model.TimeMap{defaultMap(
			on(time.Monday, 9, 0, 10, 0),
			on(time.Tuesday, 9, 0, 10, 0),
			on(time.Wednesday, 9, 0, 10, 0),
			on(time.Thursday, 9, 0, 10, 0),
			on(time.Friday, 9, 0, 10, 0),
		)}, wantSlots: 20},
		{name: "explicit primary", taskMaps: []string{"proj"}, maps: []model.TimeMap{primaryMap(on(time.Monday, 9, 0, 10, 0))}, wantSlots: 4},
		{name: "sub granularity window", maps: []model.TimeMap{defaultMap(on(time.Monday, 9, 0, 9, 10))}, wantSlots: 0},
		{name: "partial slot", maps: []model.TimeMap{defaultMap(on(time.Monday, 9, 0, 9, 20))}, wantSlots: 1},
		{name: "overlapping windows count once", maps: []model.TimeMap{defaultMap(
			on(time.Monday, 9, 0, 10, 0),
			on(time.Monday, 9, 30, 10, 30),
		)}, wantSlots: 6},
		{name: "tier overlap counts once", maps: []model.TimeMap{defaultMap(
			on(time.Monday, 9, 0, 11, 0),
			onTier(time.Monday, 10, 0, 12, 0, model.TierYellow),
		)}, wantSlots: 12},
		{name: "yellow primary skips green fallback", taskMaps: []string{"proj"}, maps: []model.TimeMap{
			primaryMap(onTier(time.Monday, 9, 0, 10, 0, model.TierYellow)),
			defaultMap(on(time.Monday, 13, 0, 14, 0)),
		}, wantSlots: 4},
		{name: "yellow primary uses yellow fallback", taskMaps: []string{"proj"}, maps: []model.TimeMap{
			primaryMap(onTier(time.Monday, 9, 0, 10, 0, model.TierYellow)),
			defaultMap(onTier(time.Monday, 13, 0, 14, 0, model.TierYellow)),
		}, wantSlots: 8},
		{name: "red primary skips yellow fallback", taskMaps: []string{"proj"}, maps: []model.TimeMap{
			primaryMap(onTier(time.Monday, 9, 0, 10, 0, model.TierRed)),
			defaultMap(onTier(time.Monday, 13, 0, 14, 0, model.TierYellow)),
		}, wantSlots: 4},
		{name: "red primary uses red fallback", taskMaps: []string{"proj"}, maps: []model.TimeMap{
			primaryMap(onTier(time.Monday, 9, 0, 10, 0, model.TierRed)),
			defaultMap(onTier(time.Monday, 13, 0, 14, 0, model.TierRed)),
		}, wantSlots: 8},
		{name: "empty primary map spills from green", taskMaps: []string{"proj"}, maps: []model.TimeMap{
			primaryMap(),
			defaultMap(on(time.Monday, 13, 0, 14, 0)),
		}, wantSlots: 4},
		{name: "DST gap has no slots", now: dstNow, maps: []model.TimeMap{defaultMap(on(time.Sunday, 1, 0, 2, 0))}, wantSlots: 0},
		{name: "DST day keeps wall clock hours", now: dstNow, maps: []model.TimeMap{defaultMap(on(time.Sunday, 9, 0, 10, 0))}, wantSlots: 4},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			now := tc.now
			if now.IsZero() {
				now = at(9, 0, 0)
			}
			g := grid.Build(now, london, 15*time.Minute, weeklyDays)
			sets := newTimeMapSets(capacityTask(tc.taskMaps...), indexTimeMaps(tc.maps))
			if got := sets.eligibleSlots(g); got != tc.wantSlots {
				t.Fatalf("eligibleSlots = %d, want %d", got, tc.wantSlots)
			}

			in := newInput(capacityTask(tc.taskMaps...))
			in.Now = now
			in.TimeMaps = tc.maps
			schedule, err := Plan(in)
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if placed := len(schedule.Blocks) > 0; placed != (tc.wantSlots > 0) {
				t.Fatalf("Plan placed=%t with %d eligible slots in %d blocks", placed, tc.wantSlots, len(schedule.Blocks))
			}
		})
	}
}

// TestTimeMapCapacityMonotone asserts that adding hours never removes
// eligible slots, including a better-tier window in the fallback set and a
// window too short for one slot.
//
// One addition is exempt: a yellow or red window added to a primary set with
// no windows at all. An empty set ranks green by convention, so the addition
// declares the Task's tier and withdraws the green fallback (ADR 0014). That
// is an authored tier declaration, not extra hours.
func TestTimeMapCapacityMonotone(t *testing.T) {
	g := grid.Build(at(9, 0, 0), london, 15*time.Minute, weeklyDays)
	bases := []struct {
		name     string
		taskMaps []string
		maps     []model.TimeMap
	}{
		{
			name: "default green",
			maps: []model.TimeMap{{Slug: "deep", Default: true, Windows: []model.Window{on(time.Monday, 9, 0, 10, 0)}}},
		},
		{
			name:     "yellow project with green default",
			taskMaps: []string{"proj"},
			maps: []model.TimeMap{
				{Slug: "proj", Windows: []model.Window{onTier(time.Monday, 9, 0, 10, 0, model.TierYellow)}},
				{Slug: "deep", Default: true, Windows: []model.Window{on(time.Monday, 13, 0, 14, 0)}},
			},
		},
		{
			name:     "empty project",
			taskMaps: []string{"proj"},
			maps: []model.TimeMap{
				{Slug: "proj"},
				{Slug: "deep", Default: true, Windows: []model.Window{on(time.Monday, 13, 0, 14, 0)}},
			},
		},
	}
	additions := []model.Window{
		on(time.Monday, 15, 0, 16, 0),
		onTier(time.Monday, 15, 0, 16, 0, model.TierYellow),
		onTier(time.Monday, 15, 0, 16, 0, model.TierRed),
		on(time.Monday, 9, 0, 9, 10),
		{Days: nil, StartMinute: 9 * 60, EndMinute: 10 * 60},
	}

	for _, base := range bases {
		t.Run(base.name, func(t *testing.T) {
			for i := range base.maps {
				for _, addition := range additions {
					before := newTimeMapSets(capacityTask(base.taskMaps...), indexTimeMaps(base.maps))
					hasPrimaryWindows := slices.ContainsFunc(before.primary, func(m model.TimeMap) bool {
						return len(m.Windows) > 0
					})
					isPrimary := slices.ContainsFunc(before.primary, func(m model.TimeMap) bool {
						return m.Slug == base.maps[i].Slug
					})
					if isPrimary && !hasPrimaryWindows && tierRank(windowTier(addition)) > 0 {
						continue
					}

					mutated := slices.Clone(base.maps)
					mutated[i].Windows = append(slices.Clone(base.maps[i].Windows), addition)
					after := newTimeMapSets(capacityTask(base.taskMaps...), indexTimeMaps(mutated))
					beforeMask := before.eligibleMask(g)
					afterMask := after.eligibleMask(g)
					if afterMask.Count() < beforeMask.Count() {
						t.Fatalf("adding %+v to map %d reduced capacity from %d to %d", addition, i, beforeMask.Count(), afterMask.Count())
					}
					if lost := beforeMask.AndNot(afterMask).Count(); lost != 0 {
						t.Fatalf("adding %+v to map %d removed %d slots", addition, i, lost)
					}
				}
			}
		})
	}
}
