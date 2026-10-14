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
func (s OrgStamp) Instant(loc *time.Location) time.Time {
	return time.Date(s.Date.Year, s.Date.Month, s.Date.Day, s.Hour, s.Minute, 0, 0, loc)
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
