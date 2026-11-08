package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/run"
)

// runSchedule implements lodo schedule: reconcile first, compute, write the
// calendar before committing the store (ADR 0007). --dry-run is forecast:
// no writes, no store commit. Before any of that, a missing Time Maps page
// is offered with the recommended defaults on confirmation (or -y).
func runSchedule(opts *options, ov configOverrides, dryRun, strict, verbose, yes bool) error {
	if dryRun {
		return runForecast(opts, ov, verbose, yes)
	}
	start := logRunStart(opts, "schedule")
	style := newStyler(opts.out, opts.disableColors)
	cfg, sources, creds, err := loadSources(opts, ov)
	if err != nil {
		return &exitError{code: codeError, err: err}
	}
	if err := checkLogseqAvailable(context.Background(), sources, makeLogseqProbeNotify(opts, "schedule")); err != nil {
		return &exitError{code: codeError, err: err}
	}
	if _, err := ensureTimeMapsPage(context.Background(), sources[0].Client, cfg.Logseq.PrimaryTimeMapsPage(), yes, errWriter(opts), newStyler(errWriter(opts), opts.disableColors)); err != nil {
		return &exitError{code: codeError, err: err}
	}
	cal, err := calendarFull(context.Background(), creds, cfg)
	if err != nil {
		return &exitError{code: codeError, err: err}
	}
	st, err := openStore()
	if err != nil {
		return &exitError{code: codeError, err: err}
	}
	defer func() { _ = st.Close() }()

	now := time.Now()
	outcome, err := run.ScheduleSources(context.Background(), sources, cal, st, cfg, now)
	if err != nil {
		return &exitError{code: codeError, err: err}
	}

	if opts.json {
		view := scheduleReport{
			Blocks:      blockViews(outcome.Tasks, outcome.Schedule.Blocks),
			Shortfalls:  shortfallViews(outcome.Tasks, outcome.Schedule.Report.Tasks),
			Suggestions: suggestionViews(outcome.Tasks, outcome.Schedule.Suggestions),
			Writes:      scheduleWrites(outcome),
			Orphans:     orphanViews(outcome.Tasks, outcome.Orphans),
			Busy:        busyView(outcome.Busy),
		}
		if verbose {
			view.Explanations = explanationViews(outcome.Schedule.Explanations)
			view.Reconciled = reconcileSummary(outcome)
			view.Dispositions = dispositionViews(outcome.Tasks, outcome.Dispositions)
		}
		if err := printJSON(opts.out, view); err != nil {
			return &exitError{code: codeError, err: err}
		}
	} else {
		renderScheduleText(opts.out, cfg, outcome, now, verbose, style)
	}
	logBusyWarnings(opts, "schedule", outcome.Busy)
	for _, o := range outcome.Orphans {
		logRunWarn(opts, "schedule", "schedule.orphan", "orphaned managed event removed", map[string]any{
			"event_id": o.EventID,
		})
	}
	logRunFinish(opts, "schedule", start, map[string]any{
		"blocks":     len(outcome.Schedule.Blocks),
		"shortfalls": len(outcome.Schedule.Report.Tasks),
		"writes":     len(outcome.Writes),
		"orphaned":   len(outcome.Orphans),
	})

	if strict && scheduleProblems(outcome) > 0 {
		return &exitError{code: codeValidation, err: fmt.Errorf(
			"schedule --strict: %d problem(s) (shortfalls, orphans and deletions count)",
			scheduleProblems(outcome))}
	}
	return nil
}

// scheduleProblems counts what --strict treats as validation failures:
// planner shortfalls plus orphaned events (Task gone, unresolvable
// pointers). Infeasibility is otherwise reported, not failed.
func scheduleProblems(outcome run.ScheduleOutcome) int {
	return len(outcome.Schedule.Report.Tasks) + len(outcome.Orphans)
}

// The JSON views are structs only; no map-keyed output.

type scheduleReport struct {
	Blocks       []blockJSON       `json:"blocks,omitempty"`
	Shortfalls   []shortfallJSON   `json:"shortfalls,omitempty"`
	Suggestions  []suggestionJSON  `json:"suggestions,omitempty"`
	Explanations []explanationJSON `json:"explanations,omitempty"`
	Writes       []writeJSON       `json:"writes,omitempty"`
	Orphans      []orphanJSON      `json:"orphaned,omitempty"`
	Reconciled   *reconcileJSON    `json:"reconciled,omitempty"`
	Dispositions []dispositionJSON `json:"dispositions,omitempty"`
	Busy         busyJSON          `json:"busy_sources"`
}

type writeJSON struct {
	Op         string    `json:"op"`
	EventID    string    `json:"event_id"`
	TaskID     string    `json:"task_id,omitempty"`
	Task       string    `json:"task,omitempty"`
	Graph      string    `json:"graph,omitempty"`
	BlockIndex int       `json:"block_index,omitempty"`
	Start      time.Time `json:"start,omitempty"`
	End        time.Time `json:"end,omitempty"`
	Summary    string    `json:"summary,omitempty"`
}

type orphanJSON struct {
	EventID string `json:"event_id"`
	TaskID  string `json:"task_id,omitempty"`
	Task    string `json:"task,omitempty"`
	Graph   string `json:"graph,omitempty"`
	Block   int    `json:"block"`
	Summary string `json:"summary,omitempty"`
}

type reconcileJSON struct {
	BindingsUpserted int `json:"bindings_upserted"`
	BindingsDeleted  int `json:"bindings_deleted"`
	Dispositions     int `json:"dispositions"`
}

type dispositionJSON struct {
	TaskID     string    `json:"task_id"`
	Task       string    `json:"task"`
	Graph      string    `json:"graph,omitempty"`
	BlockIndex int       `json:"block_index"`
	Kind       string    `json:"kind"`
	Start      time.Time `json:"start,omitempty"`
	End        time.Time `json:"end,omitempty"`
	Slots      int       `json:"slots,omitempty"`
}

func scheduleWrites(outcome run.ScheduleOutcome) []writeJSON {
	byText := textByTasks(outcome.Tasks)
	byGraph := graphByTasks(outcome.Tasks)
	out := make([]writeJSON, 0, len(outcome.Writes))
	for _, w := range outcome.Writes {
		out = append(out, writeJSON{
			Op: w.Op, EventID: w.EventID, TaskID: w.TaskID, Graph: graphForWrite(outcome.Tasks, byGraph, w),
			Task:       byText[w.TaskID],
			BlockIndex: w.BlockIndex, Start: w.Start, End: w.End, Summary: w.Summary,
		})
	}
	return out
}

func graphForWrite(tasks []model.Task, byGraph map[string]string, w run.WriteOp) string {
	if w.Graph != "" {
		return w.Graph
	}
	return byGraph[w.TaskID]
}

func reconcileSummary(outcome run.ScheduleOutcome) *reconcileJSON {
	return &reconcileJSON{
		BindingsUpserted: len(outcome.Reconciled.BindingUpserts),
		BindingsDeleted:  len(outcome.Reconciled.BindingDeletes),
		Dispositions:     len(outcome.Reconciled.DispositionUpserts),
	}
}

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
				if a.TaskID != b.TaskID {
					return strings.Compare(a.TaskID, b.TaskID)
				}
				return a.BlockIndex - b.BlockIndex
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
