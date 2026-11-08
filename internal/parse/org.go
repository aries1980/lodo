package parse

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/model"
)

// OrgStamp is a parsed Logseq org timestamp, e.g. <2026-10-20 Tue 09:00>.
type OrgStamp struct {
	Date    model.CivilDate
	Hour    int
	Minute  int
	HasTime bool
}

// Instant resolves the stamp in loc. A day-only stamp resolves to midnight.
// A local wall time that does not exist (the spring-forward gap) or occurs
// twice (the fall-back overlap) is an error: a Pin names one exact instant,
// and silently normalising or picking one of two would move the user's Block.
func (s OrgStamp) Instant(loc *time.Location) (time.Time, error) {
	if loc == nil {
		return time.Time{}, fmt.Errorf("parse org timestamp: no location")
	}
	t := time.Date(s.Date.Year, s.Date.Month, s.Date.Day, s.Hour, s.Minute, 0, 0, loc)
	// time.Date normalises a nonexistent wall time past the gap; a
	// round-trip mismatch is how that shows.
	if y, m, d := t.Date(); y != s.Date.Year || m != s.Date.Month || d != s.Date.Day ||
		t.Hour() != s.Hour || t.Minute() != s.Minute {
		return time.Time{}, fmt.Errorf("local time %s does not exist in %s (the clocks spring forward); pick another time",
			s.wallString(), loc)
	}
	// An ambiguous wall time maps to two instants. Adding a small offset
	// across the transition lands on the other occurrence and leaves the
	// wall-clock fields unchanged.
	for delta := -3 * time.Hour; delta <= 3*time.Hour; delta += 15 * time.Minute {
		if delta == 0 {
			continue
		}
		u := t.Add(delta)
		if sameWall(u, t) {
			return time.Time{}, fmt.Errorf("local time %s occurs twice in %s (the clocks fall back); pick a time that is unambiguous",
				s.wallString(), loc)
		}
	}
	return t, nil
}

// wallString renders the stamp's local wall time for diagnostics.
func (s OrgStamp) wallString() string {
	if s.HasTime {
		return fmt.Sprintf("%04d-%02d-%02d %02d:%02d", s.Date.Year, s.Date.Month, s.Date.Day, s.Hour, s.Minute)
	}
	return fmt.Sprintf("%04d-%02d-%02d", s.Date.Year, s.Date.Month, s.Date.Day)
}

// sameWall reports whether two instants read as the same local wall time.
func sameWall(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd &&
		a.Hour() == b.Hour() && a.Minute() == b.Minute()
}

var orgWeekdays = [...]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// ParseOrgStamp parses the org timestamps Logseq writes into scheduled:: and
// deadline::, in the form <2026-10-20 Tue 09:00> or <2026-10-20 Tue>.
func ParseOrgStamp(s string) (OrgStamp, error) {
	v := strings.TrimSpace(s)
	inner, ok := strings.CutPrefix(v, "<")
	if !ok {
		return OrgStamp{}, fmt.Errorf("parse org timestamp %q: missing <", s)
	}
	inner, ok = strings.CutSuffix(inner, ">")
	if !ok {
		return OrgStamp{}, fmt.Errorf("parse org timestamp %q: missing >", s)
	}

	fields := strings.Fields(inner)
	if len(fields) < 2 || len(fields) > 3 {
		return OrgStamp{}, fmt.Errorf("parse org timestamp %q: want <date weekday [time]>", s)
	}

	date, err := ParseCivilDate(fields[0])
	if err != nil {
		return OrgStamp{}, fmt.Errorf("parse org timestamp %q: %w", s, err)
	}

	want := orgWeekdays[time.Date(date.Year, date.Month, date.Day, 0, 0, 0, 0, time.UTC).Weekday()]
	if !strings.EqualFold(fields[1], want) {
		return OrgStamp{}, fmt.Errorf("parse org timestamp %q: %s is not a %s", s, fields[1], want)
	}

	stamp := OrgStamp{Date: date}
	if len(fields) == 3 {
		h, m, err := parseClock(fields[2])
		if err != nil {
			return OrgStamp{}, fmt.Errorf("parse org timestamp %q: %w", s, err)
		}
		stamp.Hour, stamp.Minute, stamp.HasTime = h, m, true
	}
	return stamp, nil
}

func parseClock(s string) (hour, minute int, err error) {
	if len(s) != 5 || s[2] != ':' || !isDigits(s[:2]) || !isDigits(s[3:]) {
		return 0, 0, fmt.Errorf("parse time %q: want HH:MM", s)
	}
	hour, _ = strconv.Atoi(s[:2])
	minute, _ = strconv.Atoi(s[3:])
	if hour > 23 || minute > 59 {
		return 0, 0, fmt.Errorf("parse time %q: want HH:MM", s)
	}
	return hour, minute, nil
}
