package cli

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/planner"
	"github.com/aries1980/lodo/internal/reconcile"
	"github.com/aries1980/lodo/internal/run"
)

// loadConfig loads config and credentials and applies the per-run overrides to
// both. It performs config/credential reads but no graph, calendar, or store
// I/O.
func loadConfig(opts *options, ov configOverrides) (config.Config, config.Credentials, error) {
	path := opts.config
	if path == "" {
		def, err := config.DefaultConfigPath()
		if err != nil {
			return config.Config{}, config.Credentials{}, fmt.Errorf("locate the config file: %w", err)
		}
		path = def
	}
	cfg, err := config.Load(path)
	if err != nil {
		return config.Config{}, config.Credentials{}, err
	}
	cfg, err = config.Apply(cfg, config.Overrides{
		WindowDays:    ov.windowDays,
		Timezone:      ov.timezone,
		ReadCalendars: ov.readCalendars,
	})
	if err != nil {
		return config.Config{}, config.Credentials{}, err
	}

	credPath, err := config.DefaultCredentialsPath()
	if err != nil {
		return config.Config{}, config.Credentials{}, fmt.Errorf("locate the credentials file: %w", err)
	}
	creds, err := config.ReadCredentials(credPath)
	if err != nil {
		return config.Config{}, config.Credentials{}, err
	}
	return cfg, creds.WithCredentialOverrides(ov.credentialOverrides()), nil
}

// buildSources resolves the Logseq token for every configured graph
// (file/LODO_LOGSEQ_TOKEN_<GRAPH>/global fallback) and constructs one
// run.Source per graph (ADR 0021). Single-graph mode returns one source with an
// empty name. It demands a token: a command that never reads the graph
// (refresh) must not call it.
func buildSources(cfg config.Config, creds config.Credentials) ([]run.Source, error) {
	credPath, err := config.DefaultCredentialsPath()
	if err != nil {
		return nil, fmt.Errorf("locate the credentials file: %w", err)
	}
	if !creds.HasAnyLogseqToken() {
		return nil, missingLogseqTokenError(credPath)
	}

	hc := httpClient(cfg)
	if len(cfg.Logseq.Graphs) == 0 {
		tok := creds.TokenForGraph("")
		if tok == "" {
			return nil, missingLogseqTokenError(credPath)
		}
		client, err := logseq.NewClient(cfg.Logseq.APIURL, tok, hc)
		if err != nil {
			return nil, err
		}
		sources := []run.Source{{Client: client, APIURL: cfg.Logseq.APIURL}}
		return sources, nil
	}

	sources := make([]run.Source, 0, len(cfg.Logseq.Graphs))
	for i, g := range cfg.Logseq.Graphs {
		tok := creds.TokenForGraph(g.Name)
		if tok == "" {
			return nil, fmt.Errorf("graph %s: no Logseq token; set logseq.graphs.%s.token in %s or %s", g.Name, g.Name, credPath, config.GraphTokenEnvVar(g.Name))
		}
		client, err := logseq.NewClient(g.APIURL, tok, hc)
		if err != nil {
			return nil, fmt.Errorf("graph %s: %w", g.Name, err)
		}
		sources = append(sources, run.Source{
			Name:           g.Name,
			Order:          i,
			Client:         client,
			DefaultTimeMap: cfg.Logseq.GraphDefault(g.Name),
			APIURL:         g.APIURL,
		})
	}
	return sources, nil
}

// missingLogseqTokenError names the credential file and the three ways to fill
// the token in.
func missingLogseqTokenError(credPath string) error {
	return fmt.Errorf("credentials file %s: logseq.token is empty; paste the Logseq API token from the app (Settings → Integrations), set %s, or pass --logseq-token", credPath, config.EnvLogseqToken)
}

// httpClient builds the transport the API adapter speaks through. An
// insecure config (self-signed app certificate) is honoured here, once:
// whoever wrote the config accepted that certificate (ADR 0005: file, not
// flag).
func httpClient(cfg config.Config) *http.Client {
	if !cfg.Logseq.Insecure {
		return nil // the adapter's default
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil
	}
	cloned := transport.Clone()
	if cloned.TLSClientConfig == nil {
		cloned.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	} else {
		tlsCfg := cloned.TLSClientConfig.Clone()
		tlsCfg.InsecureSkipVerify = true
		cloned.TLSClientConfig = tlsCfg
	}
	return &http.Client{Timeout: logseq.DefaultTimeout, Transport: cloned}
}

// errWriter is where prompts and operational notes go: stderr when the
// command wired it, stdout as a fallback, so --json stdout stays
// machine-readable.
func errWriter(opts *options) io.Writer {
	if opts.errOut != nil {
		return opts.errOut
	}
	return opts.out
}

// runCheck implements lodo check: the graph report plus the state section
// (stood-down Blocks with their Task, orphaned event ids). When the calendar
// is configured it reads real busy time — so infeasibility is judged
// against real availability — and lists orphaned events; without calendar
// credentials it falls back to the graph plus stored stood-down (the M3
// behaviour), so a fresh checkout still checks. It writes nowhere, except
// for a missing Time Maps page the user confirms (or pre-answers with -y).
func runCheck(ctx context.Context, opts *options, ov configOverrides, strict, yes bool) error {
	cmd, err := prepare(ctx, opts, ov, yes, "check")
	if err != nil {
		return fail(err)
	}
	defer cmd.close()

	outcome, err := cmd.checkOutcome(ctx)
	if err != nil {
		return fail(err)
	}
	return renderCheck(opts, cmd, outcome, strict)
}

// renderCheck prints one check outcome — JSON or text — then applies --strict.
// The calendar and no-calendar paths differ only in the outcome they hand in,
// so the report, the log record and the validation exit cannot drift apart.
func renderCheck(opts *options, cmd *command, outcome run.CheckOutcome, strict bool) error {
	if opts.json {
		view := checkView(outcome, cmd.cal != nil, opts.verbose)
		if err := printJSON(opts.out, view); err != nil {
			return fail(err)
		}
	} else {
		renderCheckText(opts.out, outcome, opts.verbose, cmd.style)
	}
	logBusyWarnings(opts, cmd.name, outcome.Busy)
	attrs := map[string]any{
		"graph_issues": len(outcome.Issues),
		"shortfalls":   len(outcome.Schedule.Report.Tasks),
		"stood_down":   len(outcome.StoodDown),
	}
	if cmd.cal != nil {
		attrs["orphaned"] = len(outcome.Orphans)
	} else {
		attrs["calendar"] = "none"
	}
	cmd.finish(attrs)
	if !strict {
		return nil
	}
	return checkStrictExit(outcome, cmd.cal != nil)
}

// checkStrictExit is the --strict refusal, or nil when the run is clean. A run
// without a calendar cannot report orphans, so it says nothing about them.
func checkStrictExit(outcome run.CheckOutcome, withCalendar bool) error {
	n := len(outcome.Issues) + len(outcome.Schedule.Report.Tasks) + len(outcome.StoodDown)
	msg := fmt.Sprintf("check --strict: %d graph issue(s), %d infeasible Task(s), %d stood-down",
		len(outcome.Issues), len(outcome.Schedule.Report.Tasks), len(outcome.StoodDown))
	if withCalendar {
		n += len(outcome.Orphans)
		msg += fmt.Sprintf(", %d orphaned", len(outcome.Orphans))
	}
	if n == 0 {
		return nil
	}
	return failCode(codeValidation, errors.New(msg))
}

// buildCheckOutcomeWithoutCalendar is the shared no-credentials fallback:
// the graph plus stored dispositions, planned with no busy time. No in-memory
// reconcile happens here — with no observed events, every binding would look
// deleted. Orphaned needs the calendar, so it stays empty. Used by check and
// explain so the degraded path cannot drift.
func buildCheckOutcomeWithoutCalendar(ctx context.Context, cfg config.Config, sources []run.Source, st run.Store, now time.Time) (run.CheckOutcome, error) {
	location, err := cfg.Scheduling.Location()
	if err != nil {
		return run.CheckOutcome{}, err
	}
	granularity, err := config.ParseGranularity(cfg.Scheduling.Granularity)
	if err != nil {
		return run.CheckOutcome{}, err
	}
	result, err := run.ReadSources(ctx, sources, cfg.Logseq.PrimaryTimeMapsPage(), location)
	if err != nil {
		return run.CheckOutcome{}, err
	}
	_, _, storedDisps, err := st.All()
	if err != nil {
		return run.CheckOutcome{}, err
	}
	effective := reconcile.Effective(result.Tasks, storedDisps)
	schedule, err := planner.Plan(planner.Input{
		Tasks: result.Tasks, TimeMaps: result.TimeMaps,
		Busy: nil, Dispositions: effective, Now: now,
		Granularity: granularity, WindowDays: cfg.Scheduling.WindowDays, Location: location,
	})
	if err != nil {
		return run.CheckOutcome{}, err
	}
	var stood []model.Disposition
	for _, d := range effective {
		if d.Kind == model.DispositionStoodDown {
			stood = append(stood, d)
		}
	}
	return run.CheckOutcome{
		Scheduled: run.Scheduled{
			Tasks: result.Tasks, Issues: result.Issues, Schedule: schedule,
		},
		Dispositions: effective, AllDispositions: storedDisps,
		StoodDown: stood, Provenance: result.Provenance,
	}, nil
}

// runForecast implements lodo forecast: compute and print the Schedule
// that would be written; a dry run with no writes. The calendar read is a
// hard part: resolve (M3), busy time (M4), then the planner — a bad or
// unreachable calendar is an operational error (exit 1), so the forecast
// never prints against a silently free calendar. The one write it may make
// is a missing Time Maps page the user confirms (or pre-answers with -y).
func runForecast(ctx context.Context, opts *options, ov configOverrides, verbose, yes bool) error {
	cmd, err := prepare(ctx, opts, ov, yes, "forecast")
	if err != nil {
		return fail(err)
	}
	defer cmd.close()

	outcome, err := run.ForecastSources(ctx, cmd.sources, cmd.cal, cmd.cfg, cmd.now)
	if err != nil {
		return fail(err)
	}

	if opts.json {
		view := forecastView(outcome, verbose)
		if err := printJSON(opts.out, view); err != nil {
			return fail(err)
		}
	} else {
		renderForecastText(opts.out, cmd.cfg, outcome.Scheduled, outcome.Managed, cmd.now, verbose, cmd.style)
	}
	logBusyWarnings(opts, cmd.name, outcome.Busy)
	if len(outcome.Managed) > 0 {
		logRunWarn(opts, cmd.name, "forecast.managed_hole", "managed events left out of busy mask", map[string]any{
			"managed": len(outcome.Managed),
		})
	}
	cmd.finish(map[string]any{
		"blocks":     len(outcome.Schedule.Blocks),
		"shortfalls": len(outcome.Schedule.Report.Tasks),
		"managed":    len(outcome.Managed),
	})
	return nil
}

// Text rendering, in the Scheduling timezone the blocks were built in.

// graphLabel prefixes the task text with [graph] in multi-graph mode.
func graphLabel(tasks []model.Task, id string) string {
	text := labelFor(textByTasks(tasks), id)
	for _, tk := range tasks {
		if tk.ID == id && tk.Graph != "" {
			return "[" + tk.Graph + "] " + text
		}
	}
	return text
}

func renderCheckText(w io.Writer, outcome run.CheckOutcome, verbose bool, st styler) {
	if len(outcome.Issues) == 0 {
		_, _ = fmt.Fprintln(w, st.success("Graph: no issues reported."))
	} else {
		_, _ = fmt.Fprintln(w, st.warning(fmt.Sprintf("Graph: %d issues", len(outcome.Issues))))
		for _, issue := range outcome.Issues {
			_, _ = fmt.Fprintf(w, "  [%s] %s: %s\n", issue.Kind, issue.Context, issue.Detail)
		}
	}

	short := outcome.Schedule.Report.Tasks
	if len(short) == 0 {
		_, _ = fmt.Fprintln(w, st.success("Plan (real busy time): everything placeable is placed."))
	} else {
		_, _ = fmt.Fprintln(w, st.warning(fmt.Sprintf("Plan (real busy time): %d Task(s) could not be fully placed.", len(short))))
		for _, r := range short {
			line := fmt.Sprintf("  %s: %d of %d slots placed — %s",
				taskLabel(outcome.Tasks, r.TaskID), r.Placed, r.Required, r.Reason)
			if len(r.BlockedBy) > 0 {
				line += fmt.Sprintf(" (blocked by %s)", strings.Join(blockedTaskLabels(outcome.Tasks, r.BlockedBy), ", "))
			}
			_, _ = fmt.Fprintln(w, st.warning(line))
		}
	}

	if len(outcome.StoodDown) == 0 {
		_, _ = fmt.Fprintln(w, st.dim("Stood-down: none."))
	} else {
		_, _ = fmt.Fprintln(w, st.warning(fmt.Sprintf("Stood-down: %d Block(s) the user deleted, not recreated until inputs change:", len(outcome.StoodDown))))
		for _, d := range outcome.StoodDown {
			_, _ = fmt.Fprintf(w, "  %s block %d (%d slots)\n",
				taskLabel(outcome.Tasks, d.TaskID), d.BlockIndex, d.Slots)
		}
	}

	if len(outcome.Orphans) == 0 {
		_, _ = fmt.Fprintln(w, st.dim("Orphaned: none."))
	} else {
		_, _ = fmt.Fprintln(w, st.danger(fmt.Sprintf("Orphaned: %d Managed event(s) with no home in the plan:", len(outcome.Orphans))))
		byText := textByTasks(outcome.Tasks)
		for _, o := range outcome.Orphans {
			label := orphanLabel(byText, o)
			_, _ = fmt.Fprintln(w, st.danger(fmt.Sprintf("  %s — %s (block %d)", o.EventID, label, o.Block)))
		}
	}

	if verbose {
		_, _ = fmt.Fprintln(w, st.bold("Explanations:"))
		for _, e := range outcome.Schedule.Explanations {
			_, _ = fmt.Fprintf(w, "  %s block %d at %s (%d slots), %s\n",
				taskLabel(outcome.Tasks, e.TaskID), e.BlockIndex,
				e.Start.Format("Mon 15:04"), e.Slots, e.Kind)
		}
		renderDecisionSummaries(w, outcome.Schedule.Decisions, func(id string) string {
			return taskLabel(outcome.Tasks, id)
		}, st)
		_, _ = fmt.Fprintln(w, st.dim("For full reasoning use `lodo explain <task-id>` (same planner logic, per-task detail)."))
	}

	renderSuggestionText(w, outcome.Tasks, outcome.Schedule.Suggestions, func(id string) string {
		return taskLabel(outcome.Tasks, id)
	}, st)

	printBusyText(w, outcome.Busy, st)

	// Summary footer: one scannable line for humans and cron mail.
	n := len(outcome.Issues) + len(short) + len(outcome.StoodDown) + len(outcome.Orphans)
	summary := fmt.Sprintf("Summary: %d graph issue(s), %d infeasible, %d stood-down, %d orphaned",
		len(outcome.Issues), len(short), len(outcome.StoodDown), len(outcome.Orphans))
	if n == 0 {
		_, _ = fmt.Fprintln(w, st.success(summary))
	} else if len(outcome.Orphans) > 0 {
		_, _ = fmt.Fprintln(w, st.danger(summary))
	} else {
		_, _ = fmt.Fprintln(w, st.warning(summary))
	}
}

func renderForecastText(w io.Writer, cfg config.Config, scheduled run.Scheduled, managed []calendar.Event, now time.Time, verbose bool, st styler) {
	granularity, err := config.ParseGranularity(cfg.Scheduling.Granularity)
	if err != nil {
		granularity = 15 * time.Minute // unreachable after config validation
	}

	loc := schedulingLocation(cfg)
	start := now.In(loc)
	_, _ = fmt.Fprintln(w, st.header(fmt.Sprintf(
		"Schedule that would be written to calendar %q — %s through %s, %s granularity, %s:",
		cfg.Calendar.ID,
		start.Format("Mon 02 Jan 2006"),
		start.AddDate(0, 0, cfg.Scheduling.WindowDays-1).Format("Mon 02 Jan 2006"),
		durationMinutes(granularity, 1),
		loc,
	)))
	printBusyText(w, scheduled.Busy, st)

	// The known forecast hole, said plainly: Lodo's own events are not in
	// the busy mask there, and schedule's reconciliation is what folds them
	// back in.
	if n := len(managed); n > 0 {
		_, _ = fmt.Fprintln(w, st.warning(fmt.Sprintf(
			"\nNote: %d Managed event(s) on the calendar are not counted as busy yet — they are\nthe ones Lodo itself created; the schedule command's reconciliation will account for them:", n)))
		for _, e := range managed {
			summary := e.Summary
			if summary == "" {
				summary = "(no title)"
			}
			_, _ = fmt.Fprintf(w, "  %s — %s\n", e.ID, summary)
		}
	}

	if len(scheduled.Schedule.Blocks) == 0 {
		_, _ = fmt.Fprintln(w, "\nNothing to schedule.")
	}

	// Group by local day; days ascend, blocks already chronologically
	// sorted by the planner.
	type dayRow struct {
		day    model.CivilDate
		when   time.Time
		blocks []model.Block
	}
	rows := map[model.CivilDate]*dayRow{}
	var order []model.CivilDate
	for _, b := range scheduled.Schedule.Blocks {
		start := b.Start.In(loc)
		d := model.CivilDateOf(start)
		row, ok := rows[d]
		if !ok {
			row = &dayRow{day: d, when: start}
			rows[d] = row
			order = append(order, d)
		}
		row.blocks = append(row.blocks, b)
	}

	dayRows := make([]*dayRow, 0, len(order))
	for _, d := range order {
		dayRows = append(dayRows, rows[d])
	}
	slices.SortFunc(dayRows, func(a, b *dayRow) int {
		return a.day.Compare(b.day)
	})

	for _, row := range dayRows {
		_, _ = fmt.Fprintln(w, st.header(fmt.Sprintf("\n%s  %s", row.when.Format("Mon"), row.day.String())))
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		for _, b := range row.blocks {
			kind := ""
			if b.Kind == model.BlockPin {
				kind = " (pin)"
			}
			task := taskLabel(scheduled.Tasks, b.TaskID) + kind
			if b.Kind == model.BlockPin {
				task = st.warning(task)
			} else {
				// Last column: color is safe for tabwriter alignment.
				task = st.success(task)
			}
			_, _ = fmt.Fprintf(tw, "  %s-%s\t%s\t%s\n",
				b.Start.In(loc).Format("15:04"),
				b.End.In(loc).Format("15:04"),
				durationMinutes(granularity, b.Slots),
				task,
			)
		}
		_ = tw.Flush()
	}

	if short := scheduled.Schedule.Report.Tasks; len(short) > 0 {
		_, _ = fmt.Fprintln(w, st.warning("\nShortfall:"))
		for _, r := range short {
			line := fmt.Sprintf("  %s: %d of %d slots placed — %s",
				taskLabel(scheduled.Tasks, r.TaskID), r.Placed, r.Required, r.Reason)
			if len(r.BlockedBy) > 0 {
				line += fmt.Sprintf(" (blocked by %s)", strings.Join(blockedTaskLabels(scheduled.Tasks, r.BlockedBy), ", "))
			}
			_, _ = fmt.Fprintln(w, st.warning(line))
		}
	}

	renderSuggestionText(w, scheduled.Tasks, scheduled.Schedule.Suggestions, func(id string) string {
		return taskLabel(scheduled.Tasks, id)
	}, st)

	if verbose {
		_, _ = fmt.Fprintln(w, st.bold("\nExplanations:"))
		for _, e := range scheduled.Schedule.Explanations {
			line := fmt.Sprintf("  %s block %d at %s (%d slots), %s",
				taskLabel(scheduled.Tasks, e.TaskID),
				e.BlockIndex,
				e.Start.In(loc).Format("Mon 15:04"),
				e.Slots,
				e.Kind,
			)
			if !e.RunStart.IsZero() {
				line += fmt.Sprintf(" — from the free run starting %s (%d slots)",
					e.RunStart.In(loc).Format("Mon 15:04"), e.RunSlots)
			}
			_, _ = fmt.Fprintln(w, st.dim(line))
		}
		renderDecisionSummaries(w, scheduled.Schedule.Decisions, func(id string) string {
			return taskLabel(scheduled.Tasks, id)
		}, st)
		_, _ = fmt.Fprintln(w, st.dim("For full reasoning use `lodo explain <task-id>` (same planner logic, per-task detail)."))
	}

	// Summary footer for humans and cron mail.
	summary := fmt.Sprintf("Summary: %d block(s), %d shortfall(s), %d managed left out",
		len(scheduled.Schedule.Blocks), len(scheduled.Schedule.Report.Tasks), len(managed))
	if len(scheduled.Schedule.Report.Tasks) > 0 {
		_, _ = fmt.Fprintln(w, st.warning(summary))
	} else {
		_, _ = fmt.Fprintln(w, st.success(summary))
	}
}

// taskLabel prefers the Task's text, falling back to its id, and prefixes
// the graph slug in multi-graph mode.
func taskLabel(tasks []model.Task, id string) string {
	return graphLabel(tasks, id)
}

// renderDecisionSummaries prints one line per task decision in consideration
// order (then id), so --verbose shows structured reasoning, not just blocks.
// It consumes planner.TaskDecision produced by the actual Plan run (no drift).
func renderSuggestionText(w io.Writer, tasks []model.Task, suggestions []planner.Suggestion, label func(string) string, st styler) {
	if len(suggestions) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w, st.bold("\nSuggestions:"))
	for _, s := range suggestions {
		name := label(s.TaskID)
		tier := ""
		if s.Tier != "" {
			tier = fmt.Sprintf(" [%s]", s.Tier)
		}
		_, _ = fmt.Fprintf(w, "  Suggestion: %s%s — %s (%s, %s)\n", name, tier, s.Change, s.Reason, s.Impact)
	}
	_, _ = fmt.Fprintln(w, st.dim("Suggestions are advisory only; Lodo never applies them automatically."))
}

func renderDecisionSummaries(w io.Writer, decisions []planner.TaskDecision, label func(string) string, st styler) {
	if len(decisions) == 0 {
		return
	}
	ordered := append([]planner.TaskDecision(nil), decisions...)
	slices.SortFunc(ordered, compareConsiderationOrder)
	_, _ = fmt.Fprintln(w, st.bold("Decisions (consideration order):"))
	for _, d := range ordered {
		pos := ""
		if d.Ordering.Position > 0 {
			pos = fmt.Sprintf("#%d/%d ", d.Ordering.Position, d.Ordering.Total)
		} else {
			pos = "unconsidered "
		}
		kind := d.Ordering.Kind
		if kind == "" {
			kind = "flexible"
		}
		tie := d.Ordering.TieBreak
		if tie == "" {
			tie = "—"
		}
		line := fmt.Sprintf("  %s%s (%s, tie-break %s): required=%d placed=%d stood_down=%d shortfall=%d",
			pos, label(d.TaskID), kind, tie,
			d.Outcome.Required, d.Outcome.Placed, d.Outcome.StoodDown, d.Outcome.Shortfall)
		if d.Outcome.Reason != "" {
			line += " — " + d.Outcome.Reason
		}
		if len(d.Outcome.BlockedBy) > 0 {
			line += " (blocked by " + strings.Join(d.Outcome.BlockedBy, ", ") + ")"
		}
		_, _ = fmt.Fprintln(w, st.dim(line))
		if d.Ordering.TieBreakDetail != "" {
			_, _ = fmt.Fprintf(w, "    tie-break: %s\n", d.Ordering.TieBreakDetail)
		}
	}
}

// blockedTaskLabels prefers Task text for blocker IDs.
func blockedTaskLabels(tasks []model.Task, ids []string) []string {
	return blockerLabels(textByTasks(tasks), ids)
}

// durationMinutes renders a Block's span: "30m", "1h" or "1h30m".
func durationMinutes(granularity time.Duration, slots int) string {
	d := time.Duration(slots) * granularity
	h, m := int(d/time.Hour), int(d/time.Minute)%60
	switch {
	case h > 0 && m > 0:
		return strconv.Itoa(h) + "h" + strconv.Itoa(m) + "m"
	case h > 0:
		return strconv.Itoa(h) + "h"
	default:
		return strconv.Itoa(m) + "m"
	}
}

// schedulingLocation loads the location the run was made with; a failure
// here is impossible after config validation, so it degrades to UTC.
func schedulingLocation(cfg config.Config) *time.Location {
	loc, err := cfg.Scheduling.Location()
	if err != nil {
		return time.UTC
	}
	return loc
}
