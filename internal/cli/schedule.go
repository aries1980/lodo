package cli

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/run"
)

// runSchedule implements lodo schedule: reconcile first, compute, write the
// calendar before committing the store (ADR 0007). --dry-run is forecast:
// no writes, no store commit.
func runSchedule(opts *options, ov configOverrides, dryRun, strict, verbose bool) error {
	if dryRun {
		return runForecast(opts, ov, verbose)
	}
	cfg, client, creds, err := loadGraph(opts, ov)
	if err != nil {
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
	outcome, err := run.Schedule(context.Background(), client, cal, st, cfg, now)
	if err != nil {
		return &exitError{code: codeError, err: err}
	}

	if opts.json {
		view := scheduleReport{
			Blocks:     scheduleBlocks(outcome),
			Shortfalls: scheduleShortfalls(outcome),
			Writes:     scheduleWrites(outcome),
			Orphans:    scheduleOrphans(outcome),
		}
		if verbose {
			view.Explanations = scheduleExplanations(outcome)
			view.Reconciled = reconcileSummary(outcome)
			view.Dispositions = scheduleDispositions(outcome)
		}
		if err := printJSON(opts.out, view); err != nil {
			return &exitError{code: codeError, err: err}
		}
	} else {
		renderScheduleText(opts.out, cfg, outcome, now, verbose)
	}

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
	Explanations []explanationJSON `json:"explanations,omitempty"`
	Writes       []writeJSON       `json:"writes,omitempty"`
	Orphans      []orphanJSON      `json:"orphaned,omitempty"`
	Reconciled   *reconcileJSON    `json:"reconciled,omitempty"`
	Dispositions []dispositionJSON `json:"dispositions,omitempty"`
}

type writeJSON struct {
	Op         string    `json:"op"`
	EventID    string    `json:"event_id"`
	TaskID     string    `json:"task_id,omitempty"`
	Task       string    `json:"task,omitempty"`
	BlockIndex int       `json:"block_index,omitempty"`
	Start      time.Time `json:"start,omitempty"`
	End        time.Time `json:"end,omitempty"`
	Summary    string    `json:"summary,omitempty"`
}

type orphanJSON struct {
	EventID string `json:"event_id"`
	TaskID  string `json:"task_id,omitempty"`
	Task    string `json:"task,omitempty"`
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
	BlockIndex int       `json:"block_index"`
	Kind       string    `json:"kind"`
	Start      time.Time `json:"start,omitempty"`
	End        time.Time `json:"end,omitempty"`
	Slots      int       `json:"slots,omitempty"`
}

func scheduleBlocks(outcome run.ScheduleOutcome) []blockJSON {
	out := make([]blockJSON, 0, len(outcome.Schedule.Blocks))
	for _, b := range outcome.Schedule.Blocks {
		out = append(out, blockJSON{
			TaskID: b.TaskID, Task: run.TaskText(outcome.Tasks, b.TaskID),
			Index: b.Index, Start: b.Start, End: b.End, Slots: b.Slots, Kind: string(b.Kind),
		})
	}
	return out
}

func scheduleShortfalls(outcome run.ScheduleOutcome) []shortfallJSON {
	out := make([]shortfallJSON, 0, len(outcome.Schedule.Report.Tasks))
	for _, r := range outcome.Schedule.Report.Tasks {
		out = append(out, shortfallJSON{
			TaskID: r.TaskID, Task: run.TaskText(outcome.Tasks, r.TaskID),
			Required: r.Required, Placed: r.Placed, StoodDown: r.StoodDown,
			Shortfall: r.Shortfall, Reason: string(r.Reason),
		})
	}
	return out
}

func scheduleExplanations(outcome run.ScheduleOutcome) []explanationJSON {
	out := make([]explanationJSON, 0, len(outcome.Schedule.Explanations))
	for _, e := range outcome.Schedule.Explanations {
		out = append(out, explanationJSON{
			TaskID: e.TaskID, BlockIndex: e.BlockIndex, Kind: string(e.Kind),
			Start: e.Start, Slots: e.Slots, RunStart: e.RunStart, RunSlots: e.RunSlots,
		})
	}
	return out
}

func scheduleWrites(outcome run.ScheduleOutcome) []writeJSON {
	out := make([]writeJSON, 0, len(outcome.Writes))
	for _, w := range outcome.Writes {
		out = append(out, writeJSON{
			Op: w.Op, EventID: w.EventID, TaskID: w.TaskID,
			Task:       run.TaskText(outcome.Tasks, w.TaskID),
			BlockIndex: w.BlockIndex, Start: w.Start, End: w.End, Summary: w.Summary,
		})
	}
	return out
}

func scheduleOrphans(outcome run.ScheduleOutcome) []orphanJSON {
	out := make([]orphanJSON, 0, len(outcome.Orphans))
	for _, o := range outcome.Orphans {
		out = append(out, orphanJSON{
			EventID: o.EventID, TaskID: o.TaskID,
			Task:  run.TaskText(outcome.Tasks, o.TaskID),
			Block: o.Block, Summary: o.Summary,
		})
	}
	return out
}

func reconcileSummary(outcome run.ScheduleOutcome) *reconcileJSON {
	return &reconcileJSON{
		BindingsUpserted: len(outcome.Reconciled.BindingUpserts),
		BindingsDeleted:  len(outcome.Reconciled.BindingDeletes),
		Dispositions:     len(outcome.Reconciled.DispositionUpserts),
	}
}

func scheduleDispositions(outcome run.ScheduleOutcome) []dispositionJSON {
	out := make([]dispositionJSON, 0, len(outcome.Dispositions))
	for _, d := range outcome.Dispositions {
		out = append(out, dispositionView(outcome.Tasks, d))
	}
	return out
}

func dispositionView(tasks []model.Task, d model.Disposition) dispositionJSON {
	text := ""
	for _, t := range tasks {
		if t.ID == d.TaskID {
			text = t.Text
			break
		}
	}
	return dispositionJSON{
		TaskID: d.TaskID, Task: text, BlockIndex: d.BlockIndex,
		Kind: string(d.Kind), Start: d.Start, End: d.End, Slots: d.Slots,
	}
}

func renderScheduleText(w interface {
	Write([]byte) (int, error)
}, cfg config.Config, outcome run.ScheduleOutcome, now time.Time, verbose bool) {
	// Reuse the forecast day-grouping by converting to an Outcome view.
	// Schedule has no managed hole: reconciliation accounted for it.
	inner := run.Outcome{
		Tasks: outcome.Tasks, Issues: outcome.Issues, Schedule: outcome.Schedule,
	}
	renderForecastText(w, cfg, inner, now, verbose)

	if len(outcome.Writes) == 0 {
		_, _ = fmt.Fprintln(w, "\nCalendar: already in sync (no writes).")
	} else {
		_, _ = fmt.Fprintf(w, "\nCalendar writes (%d):\n", len(outcome.Writes))
		for _, wr := range outcome.Writes {
			switch wr.Op {
			case "insert":
				_, _ = fmt.Fprintf(w, "  + %s  %s block %d (%s)\n",
					wr.EventID, taskLabel(inner, wr.TaskID), wr.BlockIndex, wr.Summary)
			case "patch":
				_, _ = fmt.Fprintf(w, "  ~ %s  %s block %d → %s\n",
					wr.EventID, taskLabel(inner, wr.TaskID), wr.BlockIndex,
					wr.Start.Format("Mon 15:04"))
			case "delete":
				_, _ = fmt.Fprintf(w, "  - %s\n", wr.EventID)
			}
		}
	}
	if len(outcome.Orphans) > 0 {
		_, _ = fmt.Fprintf(w, "\nOrphaned (%d Managed event(s) with no home in the plan, removed):\n", len(outcome.Orphans))
		for _, o := range outcome.Orphans {
			label := o.TaskID
			if text := run.TaskText(outcome.Tasks, o.TaskID); text != "" {
				label = text
			} else if o.TaskID == "" {
				label = "(unreadable pointer)"
			}
			_, _ = fmt.Fprintf(w, "  %s — %s (block %d)\n", o.EventID, label, o.Block)
		}
	}
	if verbose {
		r := outcome.Reconciled
		_, _ = fmt.Fprintf(w, "\nReconciled: %d binding(s) upserted, %d deleted, %d disposition(s) recorded.\n",
			len(r.BindingUpserts), len(r.BindingDeletes), len(r.DispositionUpserts))
		if len(outcome.Dispositions) > 0 {
			_, _ = fmt.Fprintln(w, "Dispositions in force:")
			ordered := append([]model.Disposition{}, outcome.Dispositions...)
			sort.Slice(ordered, func(i, j int) bool {
				if ordered[i].TaskID != ordered[j].TaskID {
					return ordered[i].TaskID < ordered[j].TaskID
				}
				return ordered[i].BlockIndex < ordered[j].BlockIndex
			})
			for _, d := range ordered {
				_, _ = fmt.Fprintf(w, "  %s block %d: %s\n",
					taskLabel(inner, d.TaskID), d.BlockIndex, d.Kind)
			}
		}
	}
}
