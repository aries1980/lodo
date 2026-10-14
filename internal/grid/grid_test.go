package grid

import (
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/google/go-cmp/cmp"

	"github.com/aries1980/lodo/internal/model"
)

func london(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	return loc
}

func TestBuildDSTDays(t *testing.T) {
	loc := london(t)

	tests := []struct {
		name     string
		now      time.Time
		days     int
		wantLen  int
		wantFrom time.Time
		wantTo   time.Time
	}{
		{
			"spring forward is 23 hours",
			time.Date(2026, time.March, 29, 12, 0, 0, 0, loc), 1, 92,
			time.Date(2026, time.March, 29, 0, 0, 0, 0, loc),
			time.Date(2026, time.March, 30, 0, 0, 0, 0, loc),
		},
		{
			"fall back is 25 hours",
			time.Date(2026, time.October, 25, 12, 0, 0, 0, loc), 1, 100,
			time.Date(2026, time.October, 25, 0, 0, 0, 0, loc),
			time.Date(2026, time.October, 26, 0, 0, 0, 0, loc),
		},
		{
			"ordinary day is 24 hours",
			time.Date(2026, time.November, 9, 12, 0, 0, 0, loc), 1, 96,
			time.Date(2026, time.November, 9, 0, 0, 0, 0, loc),
			time.Date(2026, time.November, 10, 0, 0, 0, 0, loc),
		},
		{
			"window spans the spring transition",
			time.Date(2026, time.March, 28, 12, 0, 0, 0, loc), 4, 380,
			time.Date(2026, time.March, 28, 0, 0, 0, 0, loc),
			time.Date(2026, time.April, 1, 0, 0, 0, 0, loc),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := Build(tt.now, loc, 15*time.Minute, tt.days)
			if g.Len() != tt.wantLen {
				t.Fatalf("Len = %d, want %d", g.Len(), tt.wantLen)
			}
			if !g.WindowStart().Equal(tt.wantFrom) {
				t.Fatalf("WindowStart = %v, want %v", g.WindowStart(), tt.wantFrom)
			}
			if !g.WindowEnd().Equal(tt.wantTo) {
				t.Fatalf("WindowEnd = %v, want %v", g.WindowEnd(), tt.wantTo)
			}
			for i := 0; i < g.Len(); i++ {
				if !g.End(i).After(g.Start(i)) {
					t.Fatalf("slot %d: end %v not after start %v", i, g.End(i), g.Start(i))
				}
			}
		})
	}
}

func TestBuildStepsAbsoluteInstants(t *testing.T) {
	loc := london(t)

	// Spring forward: 01:00 GMT is 02:00 BST, so the wall clock skips an hour
	// while the absolute stepping stays on the quarter hour.
	g := Build(time.Date(2026, time.March, 29, 0, 30, 0, 0, loc), loc, 15*time.Minute, 1)
	if got := g.Start(4).Format("15:04"); got != "02:00" {
		t.Fatalf("slot 4 starts %s, want 02:00", got)
	}

	// Fall back: 01:00 local happens twice; both instances are slots.
	g = Build(time.Date(2026, time.October, 25, 0, 30, 0, 0, loc), loc, 15*time.Minute, 1)
	if got := g.Start(4).Format("15:04"); got != "01:00" {
		t.Fatalf("slot 4 starts %s, want 01:00 (first pass)", got)
	}
	if got := g.Start(8); !got.Equal(time.Date(2026, time.October, 25, 1, 0, 0, 0, time.UTC)) {
		t.Fatalf("slot 8 starts %v, want 01:00 UTC (second pass)", got)
	}
}

func at(loc *time.Location, day, hour, min int) time.Time {
	return time.Date(2026, time.November, day, hour, min, 0, 0, loc)
}

func TestOverlappingAndInside(t *testing.T) {
	loc := london(t)
	g := Build(at(loc, 9, 0, 0), loc, 15*time.Minute, 1)

	tests := []struct {
		name      string
		mask      Mask
		wantCount int
		wantFirst int
		wantLast  int
	}{
		{
			"overlap rounds out to whole slots",
			g.Overlapping(at(loc, 9, 9, 7), at(loc, 9, 9, 20)),
			2, 36, 37, // 09:00 and 09:15
		},
		{
			"overlap touching a slot boundary only takes the later slot",
			g.Overlapping(at(loc, 9, 9, 0), at(loc, 9, 9, 15)),
			1, 36, 36,
		},
		{
			"inside clips partial slots",
			g.Inside(at(loc, 9, 9, 0), at(loc, 9, 11, 0)),
			8, 36, 43,
		},
		{
			"inside drops a slot that pokes past the end",
			g.Inside(at(loc, 9, 9, 0), at(loc, 9, 9, 50)),
			3, 36, 38,
		},
		{
			"empty interval marks nothing",
			g.Inside(at(loc, 9, 9, 0), at(loc, 9, 9, 0)),
			0, 0, 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.mask.Count() != tt.wantCount {
				t.Fatalf("Count = %d, want %d", tt.mask.Count(), tt.wantCount)
			}
			if tt.wantCount == 0 {
				return
			}
			runs := tt.mask.Runs(g.Len())
			if len(runs) != 1 || runs[0].Start != tt.wantFirst || runs[0].Slots != tt.wantLast-tt.wantFirst+1 {
				t.Fatalf("Runs = %+v, want a run %d..%d", runs, tt.wantFirst, tt.wantLast)
			}
		})
	}
}

func TestNotBefore(t *testing.T) {
	loc := london(t)
	g := Build(at(loc, 9, 0, 0), loc, 15*time.Minute, 1)

	m := g.NotBefore(at(loc, 9, 21, 0))
	if got := m.Count(); got != 12 {
		t.Fatalf("Count = %d, want 12", got)
	}
	if m.Has(83) || !m.Has(84) {
		t.Fatalf("21:00 (slot 84) must be the first unblocked slot")
	}
}

func TestRuns(t *testing.T) {
	m := NewMask(10)
	m.Set(1)
	m.Set(2)
	m.Set(3)
	m.Set(7)

	want := []Run{{Start: 1, Slots: 3}, {Start: 7, Slots: 1}}
	if diff := cmp.Diff(want, m.Runs(10)); diff != "" {
		t.Fatalf("Runs mismatch (-want +got):\n%s", diff)
	}

	all := NewMask(4)
	for i := 0; i < 4; i++ {
		all.Set(i)
	}
	if got := all.Runs(3); len(got) != 1 || got[0] != (Run{Start: 0, Slots: 3}) {
		t.Fatalf("Runs must respect the slot bound, got %+v", got)
	}
}

func TestTimeMapMask(t *testing.T) {
	loc := london(t)

	tests := []struct {
		name string
		now  time.Time
		days int
		tm   model.TimeMap
		want int
	}{
		{
			"weekday mornings over a week",
			at(loc, 9, 0, 0), 7,
			model.TimeMap{Windows: []model.Window{{Days: weekdays(), StartMinute: 540, EndMinute: 660}}},
			40,
		},
		{
			"window end cuts a slot it cannot contain",
			at(loc, 9, 0, 0), 1,
			model.TimeMap{Windows: []model.Window{{Days: []time.Weekday{time.Monday}, StartMinute: 540, EndMinute: 659}}},
			7,
		},
		{
			"fall back covers both passes of the hour",
			time.Date(2026, time.October, 25, 0, 0, 0, 0, loc), 1,
			model.TimeMap{Windows: []model.Window{{Days: []time.Weekday{time.Sunday}, StartMinute: 60, EndMinute: 180}}},
			12,
		},
		{
			"a slot crossing midnight never belongs to a window",
			at(loc, 9, 0, 0), 1,
			model.TimeMap{Windows: []model.Window{{Days: []time.Weekday{time.Monday}, StartMinute: 1425, EndMinute: 1439}}},
			0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := Build(tt.now, loc, 15*time.Minute, tt.days)
			if got := g.TimeMapMask(tt.tm).Count(); got != tt.want {
				t.Fatalf("Count = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMaskOperations(t *testing.T) {
	m := NewMask(130)
	m.Set(0)
	m.Set(64)
	m.Set(129)
	if m.Count() != 3 || !m.Has(64) || m.Has(63) {
		t.Fatalf("set/has/count misbehaves: %+v", m)
	}

	clone := m.Clone()
	clone.Set(1)
	if m.Has(1) {
		t.Fatal("Clone must be independent")
	}

	other := NewMask(130)
	other.Set(129)
	if got := m.And(other).Count(); got != 1 {
		t.Fatalf("And count = %d, want 1", got)
	}
	if got := m.Or(other).Count(); got != 3 {
		t.Fatalf("Or count = %d, want 3", got)
	}
	if got := m.AndNot(other).Count(); got != 2 {
		t.Fatalf("AndNot count = %d, want 2", got)
	}

	m.OrInto(other)
	if m.Count() != 3 {
		t.Fatalf("OrInto count = %d, want 3", m.Count())
	}
}

func weekdays() []time.Weekday {
	return []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
}
