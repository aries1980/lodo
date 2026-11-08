// Check path: read-only. Resolves the graph, reconciles in memory, plans
// with active dispositions. No store commit, no calendar writes.
package run

import (
	"context"
	"time"

	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/resolve"
)

// CheckOutcome is one check run: Scheduled plus the state section.
// No store commit and no calendar writes happen here.
type CheckOutcome struct {
	Scheduled
	Dispositions    []model.Disposition // effective, what the planner saw
	AllDispositions []model.Disposition // stored plus in-memory reconciled
	Orphans         []Orphan
	StoodDown       []model.Disposition // effective stood-down, for display
	Provenance      map[string]resolve.TaskProvenance
}

// Check reads the graph, the calendar's busy time and the local store, and
// plans with active dispositions — all read-only. It reconciles in memory
// (without committing) so fresh moves shape the plan it judges, but nothing
// is written anywhere.
func Check(ctx context.Context, client logseq.Client, cal CalendarReader, runStore Store, cfg config.Config, now time.Time) (CheckOutcome, error) {
	sources := []Source{{Client: client}}
	return CheckSources(ctx, sources, cal, runStore, cfg, now)
}

// CheckSources is Check over multiple graphs.
func CheckSources(ctx context.Context, sources []Source, cal CalendarReader, runStore Store, cfg config.Config, now time.Time) (CheckOutcome, error) {
	state, err := loadCalendarState(ctx, cal, runStore, cfg, now)
	if err != nil {
		return CheckOutcome{}, err
	}
	result, combined, effective, schedule, err := resolveAndPlanSources(ctx, sources, state, cfg, now)
	if err != nil {
		return CheckOutcome{}, err
	}
	_, orphans, _ := diffPlan(result.Tasks, schedule, state.managed, state.location)
	var stood []model.Disposition
	for _, disposition := range effective {
		if disposition.Kind == model.DispositionStoodDown {
			stood = append(stood, disposition)
		}
	}
	return CheckOutcome{
		Scheduled: Scheduled{
			Tasks: result.Tasks, Issues: result.Issues, Schedule: schedule, Busy: state.busyReport,
		},
		Dispositions: effective, AllDispositions: combined,
		Orphans: orphans, StoodDown: stood, Provenance: result.Provenance,
	}, nil
}
