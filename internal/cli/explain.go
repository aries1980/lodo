package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/norm"
	"github.com/aries1980/lodo/internal/planner"
	"github.com/aries1980/lodo/internal/resolve"
	"github.com/aries1980/lodo/internal/run"
)

// explainSchemaVersion is the stable --json schema version. v2 adds ordering,
// dispositions, consideration predecessors and rejection breakdowns; all
// additive over v1 (v1 readers ignore unknown fields).
const explainSchemaVersion = 2

type explainReport struct {
	SchemaVersion int                          `json:"schema_version"`
	Context       explainContext               `json:"context"`
	Task          explainTask                  `json:"task"`
	Eligibility   planner.Eligibility          `json:"eligibility"`
	Ordering      planner.Ordering             `json:"ordering"`
	Properties    []resolve.PropertyProvenance `json:"effective_properties,omitempty"`
	Dependency    planner.Dependency           `json:"dependency_gate"`
	Window        planner.Window               `json:"plan_window"`
	TimeMaps      planner.TimeMaps             `json:"time_maps"`
	Placement     *planner.Placement           `json:"placement,omitempty"`
	Blocks        []planner.BlockDecision      `json:"final_blocks,omitempty"`
	Outcome       planner.Outcome              `json:"outcome"`
	Dispositions  []model.Disposition          `json:"dispositions,omitempty"`
	Predecessors  []string                     `json:"consideration_order_before,omitempty"`
	Spillover     *explainSpillover            `json:"spillover,omitempty"`
	Issues        []resolve.Issue              `json:"graph_issues,omitempty"`
	Report        *shortfallJSON               `json:"report,omitempty"`
	Busy          *busyJSON                    `json:"busy_sources,omitempty"`
}

type explainSpillover struct {
	PrimaryPlaced  int    `json:"primary_placed"`
	FallbackPlaced int    `json:"fallback_placed"`
	Summary        string `json:"summary"`
}

type explainContext struct {
	Command      string    `json:"command"`
	Now          time.Time `json:"now"`
	Timezone     string    `json:"timezone"`
	Granularity  string    `json:"granularity"`
	WindowDays   int       `json:"window_days"`
	Calendar     bool      `json:"calendar"`
	Reconciled   bool      `json:"reconciled"`
	Dispositions int       `json:"effective_dispositions"`
}

type explainTask struct {
	ID           string          `json:"id"`
	Text         string          `json:"text"`
	Graph        string          `json:"graph,omitempty"`
	Marker       model.Marker    `json:"marker"`
	Duration     string          `json:"duration"`
	Priority     model.Priority  `json:"priority,omitempty"`
	PlanStart    string          `json:"plan_start,omitempty"`
	PlanEnd      string          `json:"plan_end,omitempty"`
	Pin          *time.Time      `json:"pin,omitempty"`
	Depends      []string        `json:"depends,omitempty"`
	Order        model.OrderMode `json:"order,omitempty"`
	Container    bool            `json:"container,omitempty"`
	ParentID     string          `json:"parent_id,omitempty"`
	GraphOrder   int             `json:"graph_order,omitempty"`
	OutlineOrder int             `json:"outline_order"`
}

// runExplain is read-only except for the one allowed graph write: creating a
// missing Time Maps page only on confirmation (ADR 0015), like check/forecast.
// It uses the same reconcile + planner logic as check/schedule (calendar busy
// time and stored dispositions when available; degrades like check without
// calendar), so the explanation reflects actual decisions, not an approximation.
func runExplain(opts *options, ov configOverrides, query string, yes bool) error {
	start := logRunStart(opts, "explain")
	style := newStyler(opts.out, opts.disableColors)
	cfg, sources, creds, err := loadSources(opts, ov)
	if err != nil {
		return &exitError{code: codeError, err: err}
	}
	if err := checkLogseqAvailable(context.Background(), sources, makeLogseqProbeNotify(opts, "explain")); err != nil {
		return &exitError{code: codeError, err: err}
	}
	if _, err := ensureTimeMapsPage(context.Background(), sources[0].Client, cfg.Logseq.PrimaryTimeMapsPage(), yes, errWriter(opts), newStyler(errWriter(opts), opts.disableColors)); err != nil {
		return &exitError{code: codeError, err: err}
	}
	st, err := openStore()
	if err != nil {
		return &exitError{code: codeError, err: err}
	}
	defer func() { _ = st.Close() }()

	cal, err := calendarFull(context.Background(), creds, cfg)
	now := time.Now()
	var outcome run.CheckOutcome
	if err != nil {
		if strings.Contains(err.Error(), "google credentials") {
			outcome, err = buildCheckOutcomeWithoutCalendar(cfg, sources, st, now)
			if err != nil {
				return &exitError{code: codeError, err: err}
			}
		} else {
			return &exitError{code: codeError, err: err}
		}
	} else {
		outcome, err = run.CheckSources(context.Background(), sources, cal, st, cfg, now)
		if err != nil {
			return &exitError{code: codeError, err: err}
		}
	}

	task, matches, err := findTask(outcome.Tasks, query)
	if err != nil {
		return &exitError{code: codeError, err: err}
	}
	if task == nil {
		msg := fmt.Sprintf("no task matches %q", query)
		if len(matches) > 0 {
			msg = fmt.Sprintf("ambiguous query %q; matches: %s", query, formatTaskMatches(matches))
		}
		if opts.json {
			_ = printJSON(opts.out, map[string]any{
				"schema_version": explainSchemaVersion,
				"error":          msg,
				"query":          query,
				"matches":        explainTaskSummaries(matches),
			})
		} else {
			_, _ = fmt.Fprintln(errWriter(opts), style.danger("lodo:")+" "+msg)
		}
		logRunFinish(opts, "explain", start, map[string]any{"matched": false})
		return &exitError{code: codeError, err: fmt.Errorf("%s", msg)}
	}

	decision := findDecision(outcome.Schedule.Decisions, task.ID)
	if decision == nil {
		// Should not happen; synthesize a minimal decision.
		d := planner.TaskDecision{TaskID: task.ID, Text: task.Text, Graph: task.Graph}
		decision = &d
	}

	prov := outcome.Provenance[task.ID]
	report := findShortfall(outcome.Schedule.Report.Tasks, task.ID)
	issues := issuesForTask(outcome.Issues, task.ID)
	loc, _ := cfg.Scheduling.Location()
	if loc == nil {
		loc = time.UTC
	}
	gran, _ := config.ParseGranularity(cfg.Scheduling.Granularity)
	if gran == 0 {
		gran = 15 * time.Minute
	}
	predecessors := explainPredecessors(outcome.Schedule.Decisions, decision.Ordering.Position)
	spill := explainSpilloverFor(*decision)

	if opts.json {
		var rep *shortfallJSON
		if report != nil {
			r := shortfallViews(outcome.Tasks, []planner.TaskReport{*report})[0]
			rep = &r
		}
		busy := busyView(outcome.Busy)
		view := explainReport{
			SchemaVersion: explainSchemaVersion,
			Context: explainContext{
				Command: "explain", Now: now, Timezone: loc.String(),
				Granularity: gran.String(), WindowDays: cfg.Scheduling.WindowDays,
				Calendar:   outcome.Busy.WriteCalendar != "" || len(outcome.Busy.Sources) > 0,
				Reconciled: true, Dispositions: len(outcome.Dispositions),
			},
			Task: explainTask{
				ID: task.ID, Text: task.Text, Graph: task.Graph, Marker: task.Marker,
				Duration: task.Duration.String(), Priority: task.Priority,
				Pin: task.Pin, Depends: append([]string(nil), task.Depends...),
				Order: task.Order, Container: task.Container, ParentID: task.ParentID,
				GraphOrder: task.GraphOrder, OutlineOrder: task.OutlineOrder,
			},
			Eligibility:  decision.Eligibility,
			Ordering:     decision.Ordering,
			Properties:   prov.Properties,
			Dependency:   decision.Dependency,
			Window:       decision.Window,
			TimeMaps:     decision.TimeMaps,
			Placement:    decision.Placement,
			Blocks:       decision.Blocks,
			Outcome:      decision.Outcome,
			Dispositions: decision.Dispositions,
			Predecessors: predecessors,
			Spillover:    spill,
			Issues:       issues,
			Report:       rep,
			Busy:         &busy,
		}
		if task.PlanStart != nil {
			view.Task.PlanStart = task.PlanStart.String()
		}
		if task.PlanEnd != nil {
			view.Task.PlanEnd = task.PlanEnd.String()
		}
		if err := printJSON(opts.out, view); err != nil {
			return &exitError{code: codeError, err: err}
		}
	} else {
		renderExplainText(opts.out, cfg, outcome, *task, *decision, prov, report, issues, predecessors, spill, gran, now, opts.verbose, style)
	}
	logBusyWarnings(opts, "explain", outcome.Busy)
	logRunFinish(opts, "explain", start, map[string]any{"task": task.ID, "reason": decision.Outcome.Reason})
	return nil
}

func explainPredecessors(decisions []planner.TaskDecision, pos int) []string {
	if pos <= 1 {
		return nil
	}
	type pair struct {
		pos int
		id  string
	}
	var pairs []pair
	for _, d := range decisions {
		if d.Ordering.Position > 0 && d.Ordering.Position < pos {
			pairs = append(pairs, pair{pos: d.Ordering.Position, id: d.TaskID})
		}
	}
	slices.SortFunc(pairs, func(a, b pair) int { return a.pos - b.pos })
	out := make([]string, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, p.id)
	}
	return out
}

func explainSpilloverFor(d planner.TaskDecision) *explainSpillover {
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
	return &explainSpillover{PrimaryPlaced: primary, FallbackPlaced: fallback, Summary: summary}
}

func findTask(tasks []model.Task, query string) (*model.Task, []model.Task, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil, fmt.Errorf("empty query")
	}
	lq := strings.ToLower(q)
	var exact, prefix, text []model.Task
	for i := range tasks {
		t := tasks[i]
		id := strings.ToLower(t.ID)
		if id == lq {
			exact = append(exact, t)
		} else if strings.HasPrefix(id, lq) {
			prefix = append(prefix, t)
		}
		blob := strings.ToLower(t.Text + " " + t.ID)
		if t.Graph != "" {
			blob = strings.ToLower("["+t.Graph+"] "+t.Text) + " " + id
		}
		if strings.Contains(blob, lq) {
			text = append(text, t)
		}
	}
	if len(exact) == 1 {
		return &exact[0], nil, nil
	}
	if len(exact) > 1 {
		return nil, exact, nil
	}
	if len(prefix) == 1 {
		return &prefix[0], nil, nil
	}
	if len(prefix) > 1 {
		return nil, prefix, nil
	}
	if len(text) == 1 {
		return &text[0], nil, nil
	}
	if len(text) > 1 {
		return nil, text, nil
	}
	return nil, nil, nil
}

func formatTaskMatches(tasks []model.Task) string {
	parts := make([]string, 0, len(tasks))
	for _, t := range tasks {
		label := t.ID
		if t.Text != "" {
			label = t.ID + " " + t.Text
		}
		if t.Graph != "" {
			label = "[" + t.Graph + "] " + label
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, "; ")
}

func explainTaskSummaries(tasks []model.Task) []map[string]string {
	out := make([]map[string]string, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, map[string]string{"id": t.ID, "text": t.Text, "graph": t.Graph})
	}
	return out
}

func findDecision(decisions []planner.TaskDecision, id string) *planner.TaskDecision {
	for i := range decisions {
		if decisions[i].TaskID == id {
			return &decisions[i]
		}
	}
	return nil
}

func findShortfall(reports []planner.TaskReport, id string) *planner.TaskReport {
	for i := range reports {
		if reports[i].TaskID == id {
			return &reports[i]
		}
	}
	return nil
}

func issuesForTask(issues []resolve.Issue, id string) []resolve.Issue {
	var out []resolve.Issue
	lid := strings.ToLower(id)
	for _, is := range issues {
		if strings.Contains(strings.ToLower(is.Context), lid) || strings.Contains(strings.ToLower(is.Detail), lid) {
			out = append(out, is)
		}
	}
	slices.SortFunc(out, func(a, b resolve.Issue) int {
		if a.Kind != b.Kind {
			return strings.Compare(a.Kind, b.Kind)
		}
		return strings.Compare(a.Context, b.Context)
	})
	return out
}

func renderExplainText(w io.Writer, cfg config.Config, outcome run.CheckOutcome, task model.Task, d planner.TaskDecision, prov resolve.TaskProvenance, report *planner.TaskReport, issues []resolve.Issue, predecessors []string, spill *explainSpillover, gran time.Duration, now time.Time, verbose bool, st styler) {
	loc := schedulingLocation(cfg)
	_, _ = fmt.Fprintln(w, st.header("Explain: "+graphLabel(outcome.Tasks, task.ID)))
	_, _ = fmt.Fprintf(w, "  id: %s\n", task.ID)
	if task.Graph != "" {
		_, _ = fmt.Fprintf(w, "  graph: %s\n", task.Graph)
	}
	_, _ = fmt.Fprintf(w, "  marker: %s  duration: %s  container: %v\n", task.Marker, task.Duration, task.Container)
	required := norm.Units(task.Duration, gran)
	_, _ = fmt.Fprintln(w, st.bold("\nEligibility:"))
	_, _ = fmt.Fprintf(w, "  considered: %v  required: %d  stood_down: %d  pinned: %d  flexible: %d\n",
		d.Eligibility.Considered, d.Eligibility.RequiredSlots, d.Eligibility.StoodDownSlots, d.Eligibility.PinnedSlots, d.Eligibility.FlexibleSlots)
	_, _ = fmt.Fprintf(w, "  granularity: %s → %d slots (ceil duration/granularity); floor %d slots per block%s\n",
		gran, required, placementFloor(d), floorNote(d))
	if d.Eligibility.SkipReason != "" {
		_, _ = fmt.Fprintf(w, "  skip: %s\n", d.Eligibility.SkipReason)
	}
	if d.Eligibility.BeyondHorizon {
		_, _ = fmt.Fprintln(w, "  beyond_horizon: true (not due yet)")
	}

	_, _ = fmt.Fprintln(w, st.bold("\nEffective properties (resolved):"))
	if len(prov.Properties) == 0 {
		_, _ = fmt.Fprintln(w, "  (no provenance recorded)")
	}
	for _, p := range prov.Properties {
		src := p.Source
		if p.SourceID != "" {
			src += " (" + p.SourceID + ")"
		}
		inh := ""
		if p.Inherited {
			inh = ", inherited"
		}
		_, _ = fmt.Fprintf(w, "  %s = %q  [%s%s]\n", p.Property, p.Value, src, inh)
	}

	renderDispositionsText(w, d, loc, st)

	_, _ = fmt.Fprintln(w, st.bold("\nOrdering / tie-breaks (actual task ordering):"))
	_, _ = fmt.Fprintf(w, "  kind: %s  priority: %s (rank %d)  outline_order: %d  graph_order: %d  plan_end: %s\n",
		d.Ordering.Kind, d.Ordering.Priority, d.Ordering.PriorityRank, d.Ordering.OutlineOrder, d.Ordering.GraphOrder, d.Ordering.PlanEnd)
	if d.Ordering.Position > 0 {
		_, _ = fmt.Fprintf(w, "  consideration: #%d of %d\n", d.Ordering.Position, d.Ordering.Total)
	} else {
		_, _ = fmt.Fprintf(w, "  consideration: unconsidered (total %d in order)\n", d.Ordering.Total)
	}
	if d.Ordering.TieBreak != "" {
		_, _ = fmt.Fprintf(w, "  tie-break: %s\n", d.Ordering.TieBreak)
	}
	if d.Ordering.TieBreakDetail != "" {
		_, _ = fmt.Fprintf(w, "  tie-break detail: %s\n", d.Ordering.TieBreakDetail)
	}
	_, _ = fmt.Fprintln(w, "  order: dependencies first (topological), then time-bound before flexible; time-bound by plan-end, then graph-order, priority, outline-order, graph, id; flexible by graph-order, priority, outline-order, graph, id. Outline-order is the final positional tie-break among equals: moving a block re-prioritises it.")
	if len(predecessors) > 0 {
		_, _ = fmt.Fprintf(w, "  earlier in order (%d): %s\n  (earlier tasks win contested slots; the schedule is computed from scratch, so staying put is emergent, not remembered)\n", len(predecessors), strings.Join(predecessorLabels(outcome, predecessors), ", "))
	}

	_, _ = fmt.Fprintln(w, st.bold("\nDependency gate:"))
	_, _ = fmt.Fprintf(w, "  depends: %v\n", d.Dependency.Depends)
	_, _ = fmt.Fprintf(w, "  blocked: %v  reason: %s  blocked_by: %v\n", d.Dependency.Blocked, d.Dependency.BlockReason, d.Dependency.BlockedBy)
	if !d.Dependency.LowerBound.IsZero() {
		_, _ = fmt.Fprintf(w, "  dependency lower bound: %s\n", d.Dependency.LowerBound.In(loc).Format(time.RFC3339))
	}
	if d.Dependency.CycleMember {
		_, _ = fmt.Fprintln(w, st.danger("  cycle_member: true"))
	}
	if d.Dependency.PinViolation {
		_, _ = fmt.Fprintln(w, st.danger("  pin_precedes_dependency: true"))
	}

	_, _ = fmt.Fprintln(w, st.bold("\nPlan / window:"))
	_, _ = fmt.Fprintf(w, "  overdue: %v  plan_start: %s  plan_end: %s  horizon_end: %s\n",
		d.Window.Overdue, d.Window.PlanStart, d.Window.PlanEnd, d.Window.HorizonEnd)
	if !d.Window.From.IsZero() {
		_, _ = fmt.Fprintf(w, "  effective from: %s\n", d.Window.From.In(loc).Format(time.RFC3339))
	}
	if !d.Window.To.IsZero() {
		_, _ = fmt.Fprintf(w, "  effective to:   %s\n", d.Window.To.In(loc).Format(time.RFC3339))
	}
	if !d.Window.Bound.IsZero() {
		_, _ = fmt.Fprintf(w, "  bound (now/deps): %s\n", d.Window.Bound.In(loc).Format(time.RFC3339))
	}

	_, _ = fmt.Fprintln(w, st.bold("\nTime Maps / tiers (actual planner passes, ADR 0014):"))
	_, _ = fmt.Fprintf(w, "  requested: %v  primary: %v  fallback: %v  best_tier: %s  no_upward_spill: %v\n",
		d.TimeMaps.Requested, d.TimeMaps.Primary, d.TimeMaps.Fallback, d.TimeMaps.BestTier, d.TimeMaps.NoUpwardSpill)
	for _, p := range d.TimeMaps.Passes {
		role := "primary"
		if !p.Primary {
			role = "fallback"
		}
		_, _ = fmt.Fprintf(w, "  pass tier=%s %s: before=%d placed=%d after=%d floor=%d eligible_tier=%d in_window=%d free=%d runs=%d below_floor_runs=%d rejected_busy=%d (external=%d pin=%d pinned=%d placed-earlier=%d) rejected_out_of_window=%d (before-bound=%d outside-plan=%d) rejected_below_floor_slots=%d\n",
			p.Tier, role, p.SlotsBefore, p.SlotsPlaced, p.SlotsAfter, p.Floor, p.EligibleTierSlots, p.WindowSlots, p.FreeSlots, p.RunsConsidered, p.RunsBelowFloor, p.RejectedBusy, p.RejectedExternal, p.RejectedPin, p.RejectedPinned, p.RejectedPlaced, p.RejectedOutOfWindow, p.RejectedBeforeBound, p.RejectedOutsidePlan, p.RejectedBelowFloor)
		if p.Comparator != nil {
			_, _ = fmt.Fprintf(w, "    comparator (ADR 0010): completeness=%d chosen_blocks=%d keys=%v\n", p.Comparator.Completeness, p.Comparator.ChosenBlocks, p.Comparator.DecisionKeys)
			for _, a := range p.Comparator.Allocations {
				_, _ = fmt.Fprintf(w, "      alloc: %s slots=%d run_slots=%d\n", a.RunStart.In(loc).Format("Mon 15:04"), a.Slots, a.RunSlots)
			}
		}
	}
	if spill != nil {
		_, _ = fmt.Fprintf(w, "  spillover: %s\n", spill.Summary)
	}

	_, _ = fmt.Fprintln(w, st.bold("\nFinal blocks / shortfall:"))
	if len(d.Blocks) == 0 {
		_, _ = fmt.Fprintln(w, "  (no blocks)")
	}
	for _, b := range d.Blocks {
		line := fmt.Sprintf("  [%d] %s %s-%s (%d slots)", b.Index, b.Kind, b.Start.In(loc).Format("Mon 15:04"), b.End.In(loc).Format("15:04"), b.Slots)
		if !b.RunStart.IsZero() {
			line += fmt.Sprintf(" from run %s (%d slots)", b.RunStart.In(loc).Format("Mon 15:04"), b.RunSlots)
		}
		_, _ = fmt.Fprintln(w, line)
	}
	out := d.Outcome
	summary := fmt.Sprintf("  outcome: required=%d placed=%d stood_down=%d shortfall=%d reason=%q", out.Required, out.Placed, out.StoodDown, out.Shortfall, out.Reason)
	if len(out.BlockedBy) > 0 {
		summary += fmt.Sprintf(" blocked_by=%v", out.BlockedBy)
	}
	if out.Reason == "" {
		_, _ = fmt.Fprintln(w, st.success(summary))
	} else {
		_, _ = fmt.Fprintln(w, st.warning(summary))
	}

	if len(issues) > 0 {
		_, _ = fmt.Fprintln(w, st.warning("\nGraph issues:"))
		for _, is := range issues {
			_, _ = fmt.Fprintf(w, "  [%s] %s: %s\n", is.Kind, is.Context, is.Detail)
		}
	}
	if report != nil && report.Reason == planner.ReasonPinPrecedesDependency {
		_, _ = fmt.Fprintln(w, st.danger("  Note: pin precedes dependency (Pin kept at authored time; order violated)."))
	}
	if outcome.Busy.WriteCalendar == "" && len(outcome.Busy.Sources) == 0 {
		_, _ = fmt.Fprintln(w, st.dim("\nNote: no calendar context (degraded fallback) — busy time/reconcile not applied; placement may differ from `lodo check`/`schedule` with calendar access."))
	}
	if verbose {
		renderConsiderationOrder(w, outcome, d.TaskID, st)
	}
	printBusyText(w, outcome.Busy, st)
}

func placementFloor(d planner.TaskDecision) int {
	if d.Placement != nil {
		return d.Placement.Floor
	}
	if len(d.TimeMaps.Passes) > 0 {
		return d.TimeMaps.Passes[0].Floor
	}
	return 2
}

func floorNote(d planner.TaskDecision) string {
	if d.Placement != nil && d.Placement.RequiredSlots == 1 {
		return " (single-slot task: floor 1)"
	}
	return ""
}

func renderDispositionsText(w io.Writer, d planner.TaskDecision, loc *time.Location, st styler) {
	_, _ = fmt.Fprintln(w, st.bold("\nDispositions (reconciled user moves/deletes):"))
	if len(d.Dispositions) == 0 {
		_, _ = fmt.Fprintln(w, "  (none: no pinned or stood-down blocks; moves would appear here as pinned and deletes as stood-down)")
		return
	}
	for _, disp := range d.Dispositions {
		switch disp.Kind {
		case model.DispositionPinned:
			_, _ = fmt.Fprintf(w, "  pinned block %d: %s-%s (user move honoured exactly, never snapped)\n",
				disp.BlockIndex, disp.Start.In(loc).Format("Mon 15:04"), disp.End.In(loc).Format("15:04"))
		case model.DispositionStoodDown:
			_, _ = fmt.Fprintf(w, "  stood-down block %d: %d slots (user delete; not recreated until inputs change)\n",
				disp.BlockIndex, disp.Slots)
		default:
			_, _ = fmt.Fprintf(w, "  %s block %d\n", disp.Kind, disp.BlockIndex)
		}
	}
}

func predecessorLabels(outcome run.CheckOutcome, ids []string) []string {
	byText := textByTasks(outcome.Tasks)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		text := byText[id]
		if text == "" {
			text = id
		} else {
			text = id + " " + text
		}
		out = append(out, text)
	}
	return out
}

func renderConsiderationOrder(w io.Writer, outcome run.CheckOutcome, currentID string, st styler) {
	decisions := append([]planner.TaskDecision(nil), outcome.Schedule.Decisions...)
	slices.SortFunc(decisions, func(a, b planner.TaskDecision) int {
		ao, bo := a.Ordering.Position, b.Ordering.Position
		if ao == 0 && bo == 0 {
			return strings.Compare(a.TaskID, b.TaskID)
		}
		if ao == 0 {
			return 1
		}
		if bo == 0 {
			return -1
		}
		if ao != bo {
			return ao - bo
		}
		return strings.Compare(a.TaskID, b.TaskID)
	})
	_, _ = fmt.Fprintln(w, st.bold("\nConsideration order (--verbose):"))
	for _, d := range decisions {
		marker := " "
		if d.TaskID == currentID {
			marker = ">"
		}
		pos := "unconsidered"
		if d.Ordering.Position > 0 {
			pos = fmt.Sprintf("#%d/%d", d.Ordering.Position, d.Ordering.Total)
		}
		_, _ = fmt.Fprintf(w, "  %s %s %s (%s, tie-break %s): placed=%d shortfall=%d reason=%q\n",
			marker, pos, graphLabel(outcome.Tasks, d.TaskID), d.Ordering.Kind, d.Ordering.TieBreak, d.Outcome.Placed, d.Outcome.Shortfall, d.Outcome.Reason)
	}
}
