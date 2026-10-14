package model

import (
	"fmt"
	"time"
)

// CivilDate is a day-granular date in the scheduling timezone. Plan bounds and
// Deadlines are days, never instants: times belong to Pins.
type CivilDate struct {
	Year  int
	Month time.Month
	Day   int
}

// CivilDateOf returns the civil date of t in t's own location.
func CivilDateOf(t time.Time) CivilDate {
	y, m, d := t.Date()
	return CivilDate{Year: y, Month: m, Day: d}
}

// String renders the date in ISO form (2006-01-02).
func (d CivilDate) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

// Before reports whether d is before other.
func (d CivilDate) Before(other CivilDate) bool {
	if d.Year != other.Year {
		return d.Year < other.Year
	}
	if d.Month != other.Month {
		return d.Month < other.Month
	}
	return d.Day < other.Day
}

// AddDays returns d shifted by n calendar days.
func (d CivilDate) AddDays(n int) CivilDate {
	t := time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n)
	return CivilDateOf(t)
}

// Midnight returns the instant at which d starts in loc.
func (d CivilDate) Midnight(loc *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, loc)
}
