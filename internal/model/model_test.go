package model

import (
	"testing"
	"time"
)

func TestPriorityRank(t *testing.T) {
	if PriorityA.Rank() >= PriorityB.Rank() || PriorityB.Rank() >= PriorityC.Rank() || PriorityC.Rank() >= Priority("").Rank() {
		t.Fatal("want A < B < C < absent")
	}
	if Priority("D").Rank() != Priority("").Rank() {
		t.Fatal("unknown priorities must sort with the absent ones")
	}
}

func TestCivilDate(t *testing.T) {
	oct31 := CivilDate{Year: 2026, Month: time.October, Day: 31}

	if got := oct31.AddDays(1); got != (CivilDate{Year: 2026, Month: time.November, Day: 1}) {
		t.Fatalf("AddDays(1) = %v", got)
	}
	if got := oct31.AddDays(-31); got != (CivilDate{Year: 2026, Month: time.September, Day: 30}) {
		t.Fatalf("AddDays(-31) = %v", got)
	}
	if got := oct31.AddDays(365); got != (CivilDate{Year: 2027, Month: time.October, Day: 31}) {
		t.Fatalf("AddDays(365) = %v", got)
	}

	oct20 := CivilDate{Year: 2026, Month: time.October, Day: 20}
	if !oct20.Before(oct31) || oct31.Before(oct20) || oct20.Before(oct20) {
		t.Fatal("Before is wrong")
	}

	if got := oct31.String(); got != "2026-10-31" {
		t.Fatalf("String = %q", got)
	}

	loc := time.FixedZone("UTC+1", 3600)
	if got := oct20.Midnight(loc); !got.Equal(time.Date(2026, time.October, 20, 0, 0, 0, 0, loc)) {
		t.Fatalf("Midnight = %v", got)
	}
}

func TestSnapshotDoesNotAliasTimeMaps(t *testing.T) {
	task := Task{ID: "x", TimeMaps: []string{"deep"}}
	s := task.Snapshot()
	task.TimeMaps[0] = "mutated"
	if s.TimeMaps[0] != "deep" {
		t.Fatal("Snapshot must not alias the Task's TimeMaps")
	}
}
