package run

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/grid"
)

// benchWriteCal gives every insert a fixed round-trip time, so the benchmark
// measures the write schedule rather than the fake.
type benchWriteCal struct{}

func (benchWriteCal) Busy(context.Context, *grid.Grid, time.Time) (grid.Mask, []calendar.Event, calendar.BusyReport, error) {
	return nil, nil, calendar.BusyReport{}, nil
}

func (benchWriteCal) List(context.Context, time.Time, time.Time) ([]calendar.Event, error) {
	return nil, nil
}

func (benchWriteCal) Get(context.Context, string) (calendar.Event, error) {
	return calendar.Event{}, nil
}

func (benchWriteCal) Insert(_ context.Context, e calendar.Event) (calendar.Event, error) {
	time.Sleep(2 * time.Millisecond)
	return e, nil
}

func (benchWriteCal) Patch(_ context.Context, _ string, e calendar.Event) (calendar.Event, error) {
	return e, nil
}

func (benchWriteCal) Delete(context.Context, string) error { return nil }

func BenchmarkApplyWrites(b *testing.B) {
	writes := make([]WriteOp, 20)
	for i := range writes {
		writes[i] = WriteOp{Op: "insert", EventID: fmt.Sprintf("event-%02d", i)}
	}
	cal := benchWriteCal{}
	b.ResetTimer()
	for b.Loop() {
		if err := applyWrites(b.Context(), cal, writes); err != nil {
			b.Fatal(err)
		}
	}
}
