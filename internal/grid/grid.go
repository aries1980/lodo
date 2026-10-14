// Package grid discretises the Scheduling window onto a granularity slot grid.
//
// The grid is built day by day in a location: slots are granularity-long
// absolute instants stepped from each local midnight to the next. A 23-hour
// DST day therefore yields 92 slots and a 25-hour day 100 at 15m — the
// integer-exact behaviour the placement comparator leans on (ADR 0008).
package grid

import (
	"sort"
	"time"

	"github.com/aries1980/lodo/internal/model"
)

// Grid is the Scheduling window as a sequence of equal-length slots.
type Grid struct {
	location    *time.Location
	granularity time.Duration
	starts      []time.Time
}

// Build lays windowDays days, starting on the day of now in loc, onto a grid
// of granularity slots. Slots never cross or extend past a local midnight, so
// a day whose length is not a whole multiple of granularity loses the
// remainder.
func Build(now time.Time, loc *time.Location, granularity time.Duration, windowDays int) *Grid {
	g := &Grid{location: loc, granularity: granularity}
	if loc == nil || granularity <= 0 {
		return g
	}

	y, m, d := now.In(loc).Date()
	for day := 0; day < windowDays; day++ {
		dayStart := time.Date(y, m, d+day, 0, 0, 0, 0, loc)
		nextMidnight := time.Date(y, m, d+day+1, 0, 0, 0, 0, loc)
		for t := dayStart; !t.Add(granularity).After(nextMidnight); t = t.Add(granularity) {
			g.starts = append(g.starts, t)
		}
	}
	return g
}

// Location returns the location the grid was built in.
func (g *Grid) Location() *time.Location { return g.location }

// Granularity returns the slot length.
func (g *Grid) Granularity() time.Duration { return g.granularity }

// Len returns the number of slots.
func (g *Grid) Len() int { return len(g.starts) }

// Start returns the instant at which slot i starts.
func (g *Grid) Start(i int) time.Time { return g.starts[i] }

// End returns the instant at which slot i ends.
func (g *Grid) End(i int) time.Time { return g.starts[i].Add(g.granularity) }

// WindowStart returns the instant the grid begins.
func (g *Grid) WindowStart() time.Time { return g.Start(0) }

// WindowEnd returns the instant the grid ends.
func (g *Grid) WindowEnd() time.Time { return g.End(g.Len() - 1) }

// MaskWords returns the number of words a Mask over this grid needs.
func (g *Grid) MaskWords() int { return (g.Len() + 63) / 64 }

// Overlapping marks every slot that overlaps [start, end). Busy time, Pins and
// Pinned Blocks project this way: any overlap blocks the whole slot.
func (g *Grid) Overlapping(start, end time.Time) Mask {
	m := NewMask(g.Len())
	if !start.Before(end) {
		return m
	}
	for i := g.firstEndingAfter(start); i < len(g.starts) && g.starts[i].Before(end); i++ {
		m.Set(i)
	}
	return m
}

// Inside marks every slot lying wholly within [start, end). Plan windows clip
// this way, so a Block can never reach past a bound.
func (g *Grid) Inside(start, end time.Time) Mask {
	m := NewMask(g.Len())
	if !start.Before(end) {
		return m
	}
	for i := g.firstStartingAtOrAfter(start); i < len(g.starts) && !g.End(i).After(end); i++ {
		m.Set(i)
	}
	return m
}

// NotBefore marks every slot that starts at or after t.
func (g *Grid) NotBefore(t time.Time) Mask {
	m := NewMask(g.Len())
	for i := g.firstStartingAtOrAfter(t); i < len(g.starts); i++ {
		m.Set(i)
	}
	return m
}

// TimeMapMask marks the slots inside any of tm's windows. Windows recur weekly
// as local wall-clock times; a slot belongs to a window only when the whole
// slot lies within it and within a single local day.
func (g *Grid) TimeMapMask(tm model.TimeMap) Mask {
	m := NewMask(g.Len())
	for i := range g.starts {
		if g.inTimeMap(i, tm) {
			m.Set(i)
		}
	}
	return m
}

func (g *Grid) inTimeMap(i int, tm model.TimeMap) bool {
	start, end := g.starts[i], g.End(i)
	sy, sm, sd := start.Date()
	ey, em, ed := end.Date()
	if sy != ey || sm != em || sd != ed {
		return false
	}

	from := start.Hour()*60 + start.Minute()
	to := end.Hour()*60 + end.Minute()
	day := start.Weekday()
	for _, w := range tm.Windows {
		if w.CoversDay(day) && from >= w.StartMinute && to <= w.EndMinute {
			return true
		}
	}
	return false
}

func (g *Grid) firstEndingAfter(t time.Time) int {
	return sort.Search(len(g.starts), func(i int) bool { return g.End(i).After(t) })
}

func (g *Grid) firstStartingAtOrAfter(t time.Time) int {
	return sort.Search(len(g.starts), func(i int) bool { return !g.starts[i].Before(t) })
}
