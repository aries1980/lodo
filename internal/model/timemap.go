package model

import (
	"slices"
	"time"
)

// Tier is a Time Map window's preference tier. All tiers are treated as green
// in v0.1; they parse so authored notes need no migration when tiered fitting
// is built.
type Tier string

const (
	TierGreen  Tier = "green"
	TierYellow Tier = "yellow"
	TierRed    Tier = "red"
)

// Window is one weekly-recurring window of a TimeMap: a set of weekdays and a
// minute-of-day interval [StartMinute, EndMinute) within a single day.
type Window struct {
	Days        []time.Weekday
	StartMinute int
	EndMinute   int
	Tier        Tier
}

// CoversDay reports whether the Window recurs on d.
func (w Window) CoversDay(d time.Weekday) bool {
	return slices.Contains(w.Days, d)
}

// TimeMap is a named, weekly-recurring pattern of hours in which the user is
// willing to do a category of work.
type TimeMap struct {
	Slug    string
	Default bool // the fallback for Tasks with no explicit time-map::
	Windows []Window
}
