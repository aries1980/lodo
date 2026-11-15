package cli

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/run"
)

// runSchedule implements lodo schedule: reconcile first, compute, write the
// calendar before committing the store (ADR 0007). --dry-run is forecast:
// no writes, no store commit. Before any of that, a missing Time Maps page
// is offered with the recommended defaults on confirmation (or -y).
func runSchedule(ctx context.Context, opts *options, ov configOverrides, dryRun, strict, verbose, yes bool) error {
	if dryRun {
		return runForecast(ctx, opts, ov, verbose, yes)
	}
	cmd, err := prepare(ctx, opts, ov, yes, "schedule")
	if err != nil {
		return fail(err)
	}
	defer cmd.close()

	outcome, err := run.ScheduleSources(ctx, cmd.sources, cmd.cal, cmd.store, cmd.cfg, cmd.now)
	if err != nil {
		return fail(err)
	}

	if opts.json {
		view := scheduleView(outcome, verbose)
		if err := printJSON(opts.out, view); err != nil {
			return fail(err)
		}
	} else {
		renderScheduleText(opts.out, cmd.cfg, outcome, cmd.now, verbose, cmd.style)
	}
	logBusyWarnings(opts, cmd.name, outcome.Busy)
	for _, o := range outcome.Orphans {
		logRunWarn(opts, cmd.name, "schedule.orphan", "orphaned managed event removed", map[string]any{
			"event_id": o.EventID,
		})
	}
	cmd.finish(map[string]any{
		"blocks":     len(outcome.Schedule.Blocks),
		"shortfalls": len(outcome.Schedule.Report.Tasks),
		"writes":     len(outcome.Writes),
		"orphaned":   len(outcome.Orphans),
	})

	if strict && scheduleProblems(outcome) > 0 {
		return failCode(codeValidation, fmt.Errorf(
			"schedule --strict: %d problem(s) (shortfalls, orphans and deletions count)",
			scheduleProblems(outcome)))
	}
	return nil
}

// scheduleProblems counts what --strict treats as validation failures:
// planner shortfalls plus orphaned events (Task gone, unresolvable
// pointers). Infeasibility is otherwise reported, not failed.
func scheduleProblems(outcome run.ScheduleOutcome) int {
	return len(outcome.Schedule.Report.Tasks) + len(outcome.Orphans)
}

// renderScheduleText renders the forecast view plus the calendar writes and
// reconciliation the run made.

func renderScheduleText(w interface {
	Write([]byte) (int, error)
}, cfg config.Config, outcome run.ScheduleOutcome, now time.Time, verbose bool, st styler) {
	// Day grouping is the forecast renderer. Schedule has no managed hole:
	// reconciliation already accounted for Lodo's own events.
	renderForecastText(w, cfg, outcome.Scheduled, nil, now, verbose, st)

	if len(outcome.Writes) == 0 {
		_, _ = fmt.Fprintln(w, st.success("\nCalendar: already in sync (no writes)."))
	} else {
		_, _ = fmt.Fprintln(w, st.header(fmt.Sprintf("\nCalendar writes (%d):", len(outcome.Writes))))
		for _, wr := range outcome.Writes {
			switch wr.Op {
			case "insert":
				_, _ = fmt.Fprintln(w, st.success(fmt.Sprintf("  + %s  %s block %d (%s)",
					wr.EventID, taskLabel(outcome.Tasks, wr.TaskID), wr.BlockIndex, wr.Summary)))
			case "patch":
				_, _ = fmt.Fprintln(w, st.warning(fmt.Sprintf("  ~ %s  %s block %d → %s",
					wr.EventID, taskLabel(outcome.Tasks, wr.TaskID), wr.BlockIndex,
					wr.Start.Format("Mon 15:04"))))
			case "delete":
				_, _ = fmt.Fprintln(w, st.danger(fmt.Sprintf("  - %s", wr.EventID)))
			}
		}
	}
	if len(outcome.Orphans) > 0 {
		_, _ = fmt.Fprintln(w, st.danger(fmt.Sprintf("\nOrphaned (%d Managed event(s) with no home in the plan, removed):", len(outcome.Orphans))))
		byText := textByTasks(outcome.Tasks)
		for _, o := range outcome.Orphans {
			label := orphanLabel(byText, o)
			_, _ = fmt.Fprintln(w, st.danger(fmt.Sprintf("  %s — %s (block %d)", o.EventID, label, o.Block)))
		}
	}
	if verbose {
		r := outcome.Reconciled
		_, _ = fmt.Fprintln(w, "\n"+st.dim(fmt.Sprintf("Reconciled: %d binding(s) upserted, %d deleted, %d disposition(s) recorded.",
			len(r.BindingUpserts), len(r.BindingDeletes), len(r.DispositionUpserts))))
		if len(outcome.Dispositions) > 0 {
			_, _ = fmt.Fprintln(w, st.bold("Dispositions in force:"))
			ordered := append([]model.Disposition{}, outcome.Dispositions...)
			slices.SortFunc(ordered, func(a, b model.Disposition) int {
				return cmp.Or(cmp.Compare(a.TaskID, b.TaskID), cmp.Compare(a.BlockIndex, b.BlockIndex))
			})
			for _, d := range ordered {
				_, _ = fmt.Fprintf(w, "  %s block %d: %s\n",
					taskLabel(outcome.Tasks, d.TaskID), d.BlockIndex, d.Kind)
			}
		}
	}
	summary := fmt.Sprintf("Summary: %d block(s), %d write(s), %d shortfall(s), %d orphaned",
		len(outcome.Schedule.Blocks), len(outcome.Writes), len(outcome.Schedule.Report.Tasks), len(outcome.Orphans))
	if len(outcome.Schedule.Report.Tasks) > 0 || len(outcome.Orphans) > 0 {
		_, _ = fmt.Fprintln(w, st.warning(summary))
	} else {
		_, _ = fmt.Fprintln(w, st.success(summary))
	}
}
