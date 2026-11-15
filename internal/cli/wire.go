package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/jsonx"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/planner"
	"github.com/aries1980/lodo/internal/resolve"
	"github.com/aries1980/lodo/internal/run"
	"github.com/aries1980/lodo/internal/wire"
)

// This file is the JSON boundary: it maps run outcomes onto internal/wire
// payloads and owns the one stdout serializer. Text rendering and label
// helpers live in views.go.

// blockViews converts planner Blocks with resolved Task text.
func blockViews(tasks []model.Task, blocks []model.Block) []wire.Block {
	return mapTaskViews(tasks, blocks, func(b model.Block, labels taskLabels) wire.Block {
		return wire.Block{
			TaskID: b.TaskID, Task: labels.text[b.TaskID], Graph: labels.graph[b.TaskID],
			Index: b.Index, Start: b.Start, End: b.End, Slots: b.Slots, Kind: string(b.Kind),
		}
	})
}

// shortfallViews converts planner TaskReports with resolved Task text.
func shortfallViews(tasks []model.Task, reports []planner.TaskReport) []wire.Shortfall {
	return mapTaskViews(tasks, reports, func(r planner.TaskReport, labels taskLabels) wire.Shortfall {
		return wire.Shortfall{
			TaskID: r.TaskID, Task: labels.text[r.TaskID], Graph: labels.graph[r.TaskID],
			Required: r.Required, Placed: r.Placed, StoodDown: r.StoodDown,
			Shortfall: r.Shortfall, Reason: string(r.Reason),
			BlockedBy: append([]string(nil), r.BlockedBy...),
		}
	})
}

// suggestionViews converts planner suggestions with resolved Task text.
func suggestionViews(tasks []model.Task, suggestions []planner.Suggestion) []wire.Suggestion {
	return mapTaskViews(tasks, suggestions, func(s planner.Suggestion, labels taskLabels) wire.Suggestion {
		return wire.Suggestion{
			TaskID: s.TaskID, Task: labels.text[s.TaskID], Graph: labels.graph[s.TaskID],
			Reason: s.Reason, Change: s.Change, Impact: s.Impact, Tier: s.Tier,
		}
	})
}

// explanationViews converts planner explanations.
func explanationViews(expls []planner.Explanation) []wire.Explanation {
	return mapViews(expls, func(e planner.Explanation) wire.Explanation {
		return wire.Explanation{
			TaskID: e.TaskID, BlockIndex: e.BlockIndex, Kind: string(e.Kind),
			Start: e.Start, Slots: e.Slots, RunStart: e.RunStart, RunSlots: e.RunSlots,
		}
	})
}

// dispositionRows converts dispositions without a Task lookup; labels stay
// empty (refresh never resolves the graph).
func dispositionRows(disps []model.Disposition, labels taskLabels) []wire.Disposition {
	return mapViews(disps, func(d model.Disposition) wire.Disposition {
		return wire.Disposition{
			TaskID: d.TaskID, Task: labels.text[d.TaskID], Graph: labels.graph[d.TaskID],
			BlockIndex: d.BlockIndex, Kind: string(d.Kind),
			Start: d.Start, End: d.End, Slots: d.Slots,
		}
	})
}

// dispositionViews converts dispositions with resolved Task text.
func dispositionViews(tasks []model.Task, disps []model.Disposition) []wire.Disposition {
	return dispositionRows(disps, taskLabels{text: textByTasks(tasks), graph: graphByTasks(tasks)})
}

// orphanViews converts schedule orphans with resolved Task text.
func orphanViews(tasks []model.Task, orphans []run.Orphan) []wire.Orphan {
	return mapTaskViews(tasks, orphans, func(o run.Orphan, labels taskLabels) wire.Orphan {
		return wire.Orphan{
			EventID: o.EventID, TaskID: o.TaskID, Graph: labels.graph[o.TaskID],
			Task:  labels.text[o.TaskID],
			Block: o.Block, Summary: o.Summary,
		}
	})
}

// checkStoodDown converts the stood-down section of a check outcome.
func checkStoodDown(outcome run.CheckOutcome) []wire.StoodDown {
	return mapTaskViews(outcome.Tasks, outcome.StoodDown, func(d model.Disposition, labels taskLabels) wire.StoodDown {
		return wire.StoodDown{
			TaskID: d.TaskID, Task: labels.text[d.TaskID], Graph: labels.graph[d.TaskID],
			BlockIndex: d.BlockIndex, Slots: d.Slots,
		}
	})
}

// managedEvents is the forecast note's payload: the events busy deliberately
// left out, in view order.
func managedEvents(outcome run.ForecastOutcome) []wire.Managed {
	return mapViews(outcome.Managed, func(e calendar.Event) wire.Managed {
		return wire.Managed{ID: e.ID, Summary: e.Summary}
	})
}

// issuesView converts graph issues.
func issuesView(issues []resolve.Issue) []wire.Issue {
	return mapViews(issues, func(i resolve.Issue) wire.Issue {
		return wire.Issue{Kind: i.Kind, Context: i.Context, Detail: i.Detail}
	})
}

// provenanceViews converts effective/inherited properties.
func provenanceViews(props []resolve.PropertyProvenance) []wire.Provenance {
	return mapViews(props, func(p resolve.PropertyProvenance) wire.Provenance {
		return wire.Provenance{
			Property: p.Property, Value: p.Value, Source: p.Source,
			SourceID: p.SourceID, Inherited: p.Inherited,
		}
	})
}

// taskMatches renders the Tasks an ambiguous explain query matched.
func taskMatches(tasks []model.Task) []wire.TaskMatch {
	return mapViews(tasks, func(t model.Task) wire.TaskMatch {
		return wire.TaskMatch{ID: t.ID, Text: t.Text, Graph: t.Graph}
	})
}

// scheduleWrites converts the calendar writes a schedule run issued.
func scheduleWrites(outcome run.ScheduleOutcome) []wire.Write {
	byText := textByTasks(outcome.Tasks)
	byGraph := graphByTasks(outcome.Tasks)
	out := make([]wire.Write, 0, len(outcome.Writes))
	for _, w := range outcome.Writes {
		out = append(out, wire.Write{
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

func reconcileSummary(outcome run.ScheduleOutcome) *wire.Reconcile {
	return &wire.Reconcile{
		BindingsUpserted: len(outcome.Reconciled.BindingUpserts),
		BindingsDeleted:  len(outcome.Reconciled.BindingDeletes),
		Dispositions:     len(outcome.Reconciled.DispositionUpserts),
	}
}

func refreshDispositions(outcome run.RefreshOutcome) []wire.Disposition {
	return dispositionRows(outcome.Dispositions, taskLabels{})
}

func refreshAdopted(outcome run.RefreshOutcome) []wire.Binding {
	out := make([]wire.Binding, 0, len(outcome.Adopted))
	for _, b := range outcome.Adopted {
		out = append(out, wire.Binding{
			TaskID: b.TaskID, BlockIndex: b.BlockIndex, EventID: b.EventID,
			Start: b.Start, End: b.End, Slots: b.Slots,
		})
	}
	return out
}

func refreshCaptured(outcome run.RefreshOutcome) []wire.Disposition {
	return dispositionRows(outcome.Captured, taskLabels{})
}

// busyView converts a calendar read report to its JSON view. Sources stay in
// config order; warnings stay in read order.
func busyView(r calendar.BusyReport) wire.Busy {
	view := wire.Busy{
		WriteCalendar: r.WriteCalendar,
		WriteEvents:   r.WriteEvents,
		Warnings:      append([]string(nil), r.Warnings...),
	}
	for _, s := range r.Sources {
		view.Sources = append(view.Sources, wire.BusySource{
			CalendarID: s.CalendarID, Status: s.Status, Events: s.Events,
		})
	}
	if len(view.Warnings) == 0 {
		view.Warnings = nil
	}
	return view
}

// conflictsView converts refresh conflicts to their JSON view, ordered by
// event ID.
func conflictsView(conflicts []run.Conflict) []wire.Conflict {
	out := make([]wire.Conflict, 0, len(conflicts))
	for _, c := range conflicts {
		out = append(out, wire.Conflict{
			TaskID: c.TaskID, BlockIndex: c.BlockIndex, EventID: c.EventID, Summary: c.Summary,
		})
	}
	return out
}

// checkView builds the check payload. withCalendar decides whether the busy
// audit is present; verbose adds per-block explanations.
func checkView(outcome run.CheckOutcome, withCalendar, verbose bool) wire.Check {
	view := wire.Check{
		SchemaVersion: wire.CheckVersion,
		GraphIssues:   issuesView(outcome.Issues),
		Shortfalls:    shortfallViews(outcome.Tasks, outcome.Schedule.Report.Tasks),
		Suggestions:   suggestionViews(outcome.Tasks, outcome.Schedule.Suggestions),
		StoodDown:     checkStoodDown(outcome),
		Orphaned:      orphanViews(outcome.Tasks, outcome.Orphans),
	}
	if withCalendar {
		busy := busyView(outcome.Busy)
		view.Busy = &busy
	}
	if verbose {
		view.Explanations = explanationViews(outcome.Schedule.Explanations)
	}
	return view
}

// forecastView builds the forecast payload.
func forecastView(outcome run.ForecastOutcome, verbose bool) wire.Forecast {
	view := wire.Forecast{
		SchemaVersion: wire.ForecastVersion,
		Blocks:        blockViews(outcome.Tasks, outcome.Schedule.Blocks),
		Shortfalls:    shortfallViews(outcome.Tasks, outcome.Schedule.Report.Tasks),
		Suggestions:   suggestionViews(outcome.Tasks, outcome.Schedule.Suggestions),
		Managed:       managedEvents(outcome),
		Busy:          busyView(outcome.Busy),
	}
	if verbose {
		view.Explanations = explanationViews(outcome.Schedule.Explanations)
	}
	return view
}

// scheduleView builds the schedule payload. verbose adds explanations, the
// reconciliation summary, and the effective dispositions.
func scheduleView(outcome run.ScheduleOutcome, verbose bool) wire.Schedule {
	view := wire.Schedule{
		SchemaVersion: wire.ScheduleVersion,
		Blocks:        blockViews(outcome.Tasks, outcome.Schedule.Blocks),
		Shortfalls:    shortfallViews(outcome.Tasks, outcome.Schedule.Report.Tasks),
		Suggestions:   suggestionViews(outcome.Tasks, outcome.Schedule.Suggestions),
		Writes:        scheduleWrites(outcome),
		Orphans:       orphanViews(outcome.Tasks, outcome.Orphans),
		Busy:          busyView(outcome.Busy),
	}
	if verbose {
		view.Explanations = explanationViews(outcome.Schedule.Explanations)
		view.Reconciled = reconcileSummary(outcome)
		view.Dispositions = dispositionViews(outcome.Tasks, outcome.Dispositions)
	}
	return view
}

// refreshView builds the refresh payload.
func refreshView(outcome run.RefreshOutcome) wire.Refresh {
	return wire.Refresh{
		SchemaVersion: wire.RefreshVersion,
		Dispositions:  refreshDispositions(outcome),
		Adopted:       refreshAdopted(outcome),
		Captured:      refreshCaptured(outcome),
		Busy:          busyView(outcome.Busy),
		Conflicts:     conflictsView(outcome.Conflicts),
	}
}

// explainViewInput is everything the explain payload needs: the actual
// decision, the Task it explains, and the run context it was produced in.
type explainViewInput struct {
	decision     planner.TaskDecision
	task         model.Task
	provenance   resolve.TaskProvenance
	issues       []resolve.Issue
	report       *planner.TaskReport
	tasks        []model.Task
	predecessors []string
	spillover    *wire.Spillover
	busy         wire.Busy
	now          time.Time
	timezone     string
	granularity  string
	windowDays   int
	calendar     bool
	dispositions int
}

// explainView builds the explain payload from the planner's trace. The trace
// types stay domain types on purpose: the explanation must not drift from the
// placement it describes.
func explainView(in explainViewInput) wire.Explain {
	task := wire.ExplainTask{
		ID: in.task.ID, Text: in.task.Text, Graph: in.task.Graph,
		Marker: string(in.task.Marker), Duration: in.task.Duration.String(),
		Priority:   string(in.task.Priority),
		Pin:        in.task.Pin,
		Depends:    append([]string(nil), in.task.Depends...),
		Order:      string(in.task.Order),
		Container:  in.task.Container,
		ParentID:   in.task.ParentID,
		GraphOrder: in.task.GraphOrder, OutlineOrder: in.task.OutlineOrder,
	}
	if in.task.PlanStart != nil {
		task.PlanStart = in.task.PlanStart.String()
	}
	if in.task.PlanEnd != nil {
		task.PlanEnd = in.task.PlanEnd.String()
	}

	view := wire.Explain{
		SchemaVersion: wire.ExplainVersion,
		Context: wire.ExplainContext{
			Command: "explain", Now: in.now, Timezone: in.timezone,
			Granularity: in.granularity, WindowDays: in.windowDays,
			Calendar: in.calendar, Reconciled: true, Dispositions: in.dispositions,
		},
		Task:         task,
		Eligibility:  in.decision.Eligibility,
		Ordering:     in.decision.Ordering,
		Properties:   provenanceViews(in.provenance.Properties),
		Dependency:   in.decision.Dependency,
		Window:       in.decision.Window,
		TimeMaps:     in.decision.TimeMaps,
		Placement:    in.decision.Placement,
		Blocks:       in.decision.Blocks,
		Outcome:      in.decision.Outcome,
		Dispositions: dispositionViews(in.tasks, in.decision.Dispositions),
		Predecessors: in.predecessors,
		Spillover:    in.spillover,
		Issues:       issuesView(in.issues),
		Busy:         &in.busy,
	}
	if in.report != nil {
		report := shortfallViews(in.tasks, []planner.TaskReport{*in.report})[0]
		view.Report = &report
	}
	return view
}

// explainErrorView builds the payload for an ambiguous or empty explain query.
func explainErrorView(query, msg string, matches []model.Task) wire.ExplainError {
	return wire.ExplainError{
		SchemaVersion: wire.ExplainVersion,
		Error:         msg,
		Query:         query,
		Matches:       taskMatches(matches),
	}
}

// explainSpilloverFor summarizes primary versus fallback placements.
func explainSpilloverFor(d planner.TaskDecision) *wire.Spillover {
	if len(d.TimeMaps.Passes) == 0 {
		return nil
	}
	primary, fallback := 0, 0
	for _, p := range d.TimeMaps.Passes {
		if p.Primary {
			primary += p.SlotsPlaced
		} else {
			fallback += p.SlotsPlaced
		}
	}
	if primary+fallback == 0 {
		return nil
	}
	var summary string
	switch {
	case fallback == 0:
		summary = fmt.Sprintf("placed %d slots in primary maps; no spillover needed", primary)
	case primary == 0:
		summary = fmt.Sprintf("placed %d slots in fallback (default) maps; primary maps had no usable time", fallback)
	default:
		summary = fmt.Sprintf("placed %d slots in primary maps, spilled %d slots into fallback (default) maps", primary, fallback)
	}
	if d.TimeMaps.NoUpwardSpill {
		summary += "; never spilled into a better tier than the project map's best"
	}
	return &wire.Spillover{PrimaryPlaced: primary, FallbackPlaced: fallback, Summary: summary}
}

// authView builds the auth google payload.
func authView(path string) wire.Auth {
	return wire.Auth{SchemaVersion: wire.AuthVersion, Stored: path}
}

// printJSON writes indented, newline-terminated, deterministic JSON. It only
// accepts a wire payload, so a command cannot emit an ad-hoc struct or a map
// whose key order is not part of the contract.
func printJSON(w io.Writer, v wire.Payload) error {
	raw, err := jsonx.MarshalIndent(v, "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	_, err = w.Write(raw)
	return err
}
