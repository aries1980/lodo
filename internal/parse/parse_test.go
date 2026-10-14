package parse

import (
	"testing"
	"time"

	"github.com/aries1980/lodo/internal/model"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"minutes", "90m", 90 * time.Minute, false},
		{"hours", "2h", 2 * time.Hour, false},
		{"mixed", "1h30m", 90 * time.Minute, false},
		{"surrounding space", " 45m ", 45 * time.Minute, false},
		{"zero", "0m", 0, true},
		{"negative", "-5m", 0, true},
		{"garbage", "soon", 0, true},
		{"empty", "", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDuration(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseDuration(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ParseDuration(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParsePriority(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    model.Priority
		wantErr bool
	}{
		{"A", "A", model.PriorityA, false},
		{"lowercase b", "b", model.PriorityB, false},
		{"padded C", " c ", model.PriorityC, false},
		{"empty", "", "", true},
		{"out of range", "D", "", true},
		{"word", "high", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePriority(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParsePriority(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ParsePriority(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseCivilDate(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    model.CivilDate
		wantErr bool
	}{
		{"date", "2026-10-20", model.CivilDate{Year: 2026, Month: time.October, Day: 20}, false},
		{"leap day", "2028-02-29", model.CivilDate{Year: 2028, Month: time.February, Day: 29}, false},
		{"padded", " 2026-01-02 ", model.CivilDate{Year: 2026, Month: time.January, Day: 2}, false},
		{"not a date", "20/10/2026", model.CivilDate{}, true},
		{"impossible day", "2026-02-30", model.CivilDate{}, true},
		{"empty", "", model.CivilDate{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCivilDate(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseCivilDate(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ParseCivilDate(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseOrgStamp(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    OrgStamp
		wantErr bool
	}{
		{
			"with time",
			"<2026-10-20 Tue 09:00>",
			OrgStamp{Date: model.CivilDate{Year: 2026, Month: time.October, Day: 20}, Hour: 9, HasTime: true},
			false,
		},
		{
			"day only",
			"<2026-10-20 Tue>",
			OrgStamp{Date: model.CivilDate{Year: 2026, Month: time.October, Day: 20}},
			false,
		},
		{
			"weekday is case-insensitive",
			"<2026-10-20 tue 09:00>",
			OrgStamp{Date: model.CivilDate{Year: 2026, Month: time.October, Day: 20}, Hour: 9, HasTime: true},
			false,
		},
		{"wrong weekday", "<2026-10-20 Mon 09:00>", OrgStamp{}, true},
		{"missing brackets", "2026-10-20 Tue 09:00", OrgStamp{}, true},
		{"unclosed bracket", "<2026-10-20 Tue 09:00", OrgStamp{}, true},
		{"missing weekday", "<2026-10-20 09:00>", OrgStamp{}, true},
		{"missing date", "<Tue 09:00>", OrgStamp{}, true},
		{"loose time", "<2026-10-20 Tue 9:00>", OrgStamp{}, true},
		{"hour out of range", "<2026-10-20 Tue 24:00>", OrgStamp{}, true},
		{"minute out of range", "<2026-10-20 Tue 09:60>", OrgStamp{}, true},
		{"trailing junk", "<2026-10-20 Tue 09:00 extra>", OrgStamp{}, true},
		{"empty", "", OrgStamp{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseOrgStamp(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseOrgStamp(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ParseOrgStamp(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestOrgStampInstant(t *testing.T) {
	loc := time.FixedZone("UTC+1", 3600)
	stamp := OrgStamp{Date: model.CivilDate{Year: 2026, Month: time.October, Day: 20}, Hour: 9, Minute: 30, HasTime: true}
	want := time.Date(2026, time.October, 20, 9, 30, 0, 0, loc)
	if got := stamp.Instant(loc); !got.Equal(want) {
		t.Fatalf("Instant = %v, want %v", got, want)
	}

	dayOnly := OrgStamp{Date: stamp.Date}
	if got := dayOnly.Instant(loc); !got.Equal(time.Date(2026, time.October, 20, 0, 0, 0, 0, loc)) {
		t.Fatalf("day-only Instant = %v, want midnight", got)
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Deep Work", "deep-work"},
		{"deep-work", "deep-work"},
		{"Weekends", "weekends"},
		{"  Deep   Work  ", "deep-work"},
		{"R&D / Deep Work", "r-d-deep-work"},
		{"deep---work", "deep-work"},
		{"--", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := Slugify(tt.in); got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseWindow(t *testing.T) {
	weekdays := []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}

	tests := []struct {
		name    string
		in      string
		want    model.Window
		wantErr bool
	}{
		{
			"weekday range",
			"Mon-Fri 09:00-11:00",
			model.Window{Days: weekdays, StartMinute: 9 * 60, EndMinute: 11 * 60, Tier: model.TierGreen},
			false,
		},
		{
			"weekend range with tier",
			"Sat-Sun 08:00-20:00 yellow",
			model.Window{Days: []time.Weekday{time.Saturday, time.Sunday}, StartMinute: 8 * 60, EndMinute: 20 * 60, Tier: model.TierYellow},
			false,
		},
		{
			"listed days",
			"Mon,Wed,Fri 18:00-19:00",
			model.Window{Days: []time.Weekday{time.Monday, time.Wednesday, time.Friday}, StartMinute: 18 * 60, EndMinute: 19 * 60, Tier: model.TierGreen},
			false,
		},
		{
			"single day",
			"Sat 10:00-12:00 red",
			model.Window{Days: []time.Weekday{time.Saturday}, StartMinute: 10 * 60, EndMinute: 12 * 60, Tier: model.TierRed},
			false,
		},
		{"empty list element", "Mon,,Fri 09:00-11:00", model.Window{}, true},
		{"backwards range", "Fri-Mon 09:00-11:00", model.Window{}, true},
		{"backwards span", "Mon 11:00-09:00", model.Window{}, true},
		{"zero span", "Mon 09:00-09:00", model.Window{}, true},
		{"midnight crossing", "Mon 23:00-01:00", model.Window{}, true},
		{"unknown day", "Moo 09:00-11:00", model.Window{}, true},
		{"unknown tier", "Mon 09:00-11:00 blue", model.Window{}, true},
		{"missing time", "Mon", model.Window{}, true},
		{"loose time", "Mon 9:00-11:00", model.Window{}, true},
		{"hour out of range", "Mon 24:00-25:00", model.Window{}, true},
		{"minute out of range", "Mon 09:60-11:00", model.Window{}, true},
		{"trailing junk", "Mon 09:00-11:00 yellow extra", model.Window{}, true},
		{"empty", "", model.Window{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseWindow(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseWindow(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !equalWindow(got, tt.want) {
				t.Fatalf("ParseWindow(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func equalWindow(a, b model.Window) bool {
	if a.StartMinute != b.StartMinute || a.EndMinute != b.EndMinute || a.Tier != b.Tier || len(a.Days) != len(b.Days) {
		return false
	}
	for i := range a.Days {
		if a.Days[i] != b.Days[i] {
			return false
		}
	}
	return true
}
