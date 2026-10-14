package parse

import (
	"fmt"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/model"
)

// ParseWindow parses one Time Map window line, e.g. "Mon-Fri 09:00-11:00" or
// "Sat-Sun 08:00-20:00 yellow". Windows lie within a single day; one crossing
// midnight is an error, never a guess.
func ParseWindow(line string) (model.Window, error) {
	fields := strings.Fields(line)
	if len(fields) < 2 || len(fields) > 3 {
		return model.Window{}, fmt.Errorf("parse window %q: want <days> <HH:MM>-<HH:MM> [tier]", line)
	}

	days, err := parseDays(fields[0])
	if err != nil {
		return model.Window{}, fmt.Errorf("parse window %q: %w", line, err)
	}

	start, end, err := parseSpan(fields[1])
	if err != nil {
		return model.Window{}, fmt.Errorf("parse window %q: %w", line, err)
	}

	w := model.Window{Days: days, StartMinute: start, EndMinute: end, Tier: model.TierGreen}
	if len(fields) == 3 {
		tier, err := parseTier(fields[2])
		if err != nil {
			return model.Window{}, fmt.Errorf("parse window %q: %w", line, err)
		}
		w.Tier = tier
	}
	return w, nil
}

var dayNames = map[string]time.Weekday{
	"mon": time.Monday,
	"tue": time.Tuesday,
	"wed": time.Wednesday,
	"thu": time.Thursday,
	"fri": time.Friday,
	"sat": time.Saturday,
	"sun": time.Sunday,
}

// parseDays parses "Mon", "Mon-Fri" or "Mon,Wed,Fri". Ranges are ordered as in
// the week Mon..Sun, so "Sat-Sun" is a range and "Fri-Mon" is an error.
func parseDays(s string) ([]time.Weekday, error) {
	switch {
	case strings.Contains(s, ","):
		var days []time.Weekday
		seen := make(map[time.Weekday]bool)
		for _, part := range strings.Split(s, ",") {
			day, ok := dayNames[strings.ToLower(part)]
			if !ok {
				return nil, fmt.Errorf("unknown day %q", part)
			}
			if !seen[day] {
				seen[day] = true
				days = append(days, day)
			}
		}
		return days, nil
	case strings.Contains(s, "-"):
		parts := strings.SplitN(s, "-", 2)
		from, ok := dayNames[strings.ToLower(parts[0])]
		if !ok {
			return nil, fmt.Errorf("unknown day %q", parts[0])
		}
		to, ok := dayNames[strings.ToLower(parts[1])]
		if !ok {
			return nil, fmt.Errorf("unknown day %q", parts[1])
		}
		first, last := mondayFirst(from), mondayFirst(to)
		if first > last {
			return nil, fmt.Errorf("day range %q runs backwards", s)
		}
		days := make([]time.Weekday, 0, last-first+1)
		for i := first; i <= last; i++ {
			days = append(days, mondayFirstDay(i))
		}
		return days, nil
	default:
		day, ok := dayNames[strings.ToLower(s)]
		if !ok {
			return nil, fmt.Errorf("unknown day %q", s)
		}
		return []time.Weekday{day}, nil
	}
}

func mondayFirst(d time.Weekday) int { return (int(d) + 6) % 7 }

func mondayFirstDay(i int) time.Weekday { return time.Weekday((i + 1) % 7) }

// parseSpan parses "HH:MM-HH:MM" within one day.
func parseSpan(s string) (start, end int, err error) {
	from, to, ok := strings.Cut(s, "-")
	if !ok {
		return 0, 0, fmt.Errorf("parse span %q: want HH:MM-HH:MM", s)
	}
	start, err = parseMinuteOfDay(from)
	if err != nil {
		return 0, 0, err
	}
	end, err = parseMinuteOfDay(to)
	if err != nil {
		return 0, 0, err
	}
	if start >= end {
		return 0, 0, fmt.Errorf("parse span %q: must start before it ends (midnight crossing is not supported)", s)
	}
	return start, end, nil
}

func parseMinuteOfDay(s string) (int, error) {
	hour, minute, err := parseClock(s)
	if err != nil {
		return 0, err
	}
	return hour*60 + minute, nil
}

func parseTier(s string) (model.Tier, error) {
	switch model.Tier(strings.ToLower(s)) {
	case model.TierGreen, model.TierYellow, model.TierRed:
		return model.Tier(strings.ToLower(s)), nil
	default:
		return "", fmt.Errorf("unknown tier %q: want green, yellow or red", s)
	}
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
