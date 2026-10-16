package planner

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/aries1980/lodo/internal/model"
)

func onTier(day time.Weekday, fromHour, fromMin, toHour, toMin int, tier model.Tier) model.Window {
	w := on(day, fromHour, fromMin, toHour, toMin)
	w.Tier = tier
	return w
}

func TestTiers(t *testing.T) {
	tests := []struct {
		name    string
		minutes int
		windows []model.Window
		want    []string
	}{
		{
			name:    "a later green window beats an earlier yellow one",
			minutes: 120,
			windows: []model.Window{
				onTier(time.Monday, 9, 0, 11, 0, model.TierYellow),
				onTier(time.Friday, 9, 0, 11, 0, model.TierGreen),
			},
			want: []string{"Fri 09:00@120m"},
		},
		{
			name:    "hours spill into yellow when green is short",
			minutes: 120,
			windows: []model.Window{
				onTier(time.Monday, 9, 0, 10, 0, model.TierGreen),
				onTier(time.Monday, 13, 0, 15, 0, model.TierYellow),
			},
			want: []string{"Mon 09:00@60m", "Mon 13:00@60m"},
		},
		{
			name:    "a one-slot green fragment is unused and the hours spill",
			minutes: 60,
			windows: []model.Window{
				onTier(time.Monday, 9, 0, 9, 15, model.TierGreen),
				onTier(time.Monday, 13, 0, 14, 0, model.TierYellow),
			},
			want: []string{"Mon 13:00@60m"},
		},
		{
			name:    "hours spill into red when green and yellow are short",
			minutes: 120,
			windows: []model.Window{
				onTier(time.Monday, 9, 0, 10, 0, model.TierGreen),
				onTier(time.Monday, 13, 0, 15, 0, model.TierRed),
			},
			want: []string{"Mon 09:00@60m", "Mon 13:00@60m"},
		},
		{
			name:    "an overlapping worse window loses those slots",
			minutes: 180,
			windows: []model.Window{
				onTier(time.Monday, 9, 0, 11, 0, model.TierGreen),
				onTier(time.Monday, 9, 0, 12, 0, model.TierYellow),
			},
			want: []string{"Mon 09:00@120m", "Mon 11:00@60m"},
		},
		{
			name:    "an all-green map still front-loads",
			minutes: 75,
			windows: []model.Window{
				onTier(time.Monday, 9, 0, 9, 30, model.TierGreen),
				onTier(time.Tuesday, 9, 0, 10, 15, model.TierGreen),
			},
			want: []string{"Mon 09:00@30m", "Tue 09:00@45m"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := newInput(localTask("t1", duration(tt.minutes)))
			in.TimeMaps = []model.TimeMap{deepMap(tt.windows...)}
			s, err := Plan(in)
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if diff := cmp.Diff(tt.want, describe(s.Blocks)); diff != "" {
				t.Fatalf("Blocks mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDefaultMapUsesTiers(t *testing.T) {
	task := model.Task{ID: "t1", Text: "t1", Marker: model.MarkerTODO, Duration: duration(120)}
	in := newInput(task)
	in.TimeMaps = []model.TimeMap{{
		Slug:    "any",
		Default: true,
		Windows: []model.Window{
			onTier(time.Monday, 9, 0, 11, 0, model.TierYellow),
			onTier(time.Friday, 9, 0, 11, 0, model.TierGreen),
		},
	}}
	s, err := Plan(in)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	want := []string{"Fri 09:00@120m"}
	if diff := cmp.Diff(want, describe(s.Blocks)); diff != "" {
		t.Fatalf("Blocks mismatch (-want +got):\n%s", diff)
	}
}
