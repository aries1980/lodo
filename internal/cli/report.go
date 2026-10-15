package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/resolve"
	"github.com/aries1980/lodo/internal/run"
)

// loadGraph assembles the read side: config (with resolved overrides),
// credentials (strict 0600), and the Logseq HTTP API client. The
// credentials travel too, for the commands that read past the Logseq side
// of their file (forecast's calendar).
func loadGraph(opts *options, ov configOverrides) (config.Config, logseq.Client, config.Credentials, error) {
	path := opts.config
	if path == "" {
		def, err := config.DefaultConfigPath()
		if err != nil {
			return config.Config{}, nil, config.Credentials{}, fmt.Errorf("locate the config file: %w", err)
		}
		path = def
	}
	cfg, err := config.Load(path)
	if err != nil {
		return config.Config{}, nil, config.Credentials{}, err
	}
	cfg, err = config.Apply(cfg, config.Overrides{
		WindowDays: ov.windowDays,
		Timezone:   ov.timezone,
	})
	if err != nil {
		return config.Config{}, nil, config.Credentials{}, err
	}

	credPath, err := config.DefaultCredentialsPath()
	if err != nil {
		return config.Config{}, nil, config.Credentials{}, fmt.Errorf("locate the credentials file: %w", err)
	}
	creds, err := config.LoadCredentials(credPath)
	if err != nil {
		return config.Config{}, nil, config.Credentials{}, err
	}

	client, err := logseq.NewClient(cfg.Logseq.APIURL, creds.Logseq.Token, httpClient(cfg))
	if err != nil {
		return config.Config{}, nil, config.Credentials{}, err
	}
	return cfg, client, creds, nil
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
	tls := transport.Clone()
	tls.TLSClientConfig.InsecureSkipVerify = true
	return &http.Client{Timeout: logseq.DefaultTimeout, Transport: tls}
}

// readGraph is check's read side: resolve the graph, plan with no busy
// time (check never consults the calendar), Now injected once per run —
// never inside the pure packages.
func readGraph(opts *options, ov configOverrides) (config.Config, run.Outcome, time.Time, error) {
	cfg, client, _, err := loadGraph(opts, ov)
	if err != nil {
		return config.Config{}, run.Outcome{}, time.Time{}, err
	}
	now := time.Now()
	outcome, err := run.Graph(context.Background(), client, nil, cfg, now)
	if err != nil {
		return cfg, run.Outcome{}, now, err
	}
	return cfg, outcome, now, nil
}

// runCheck implements lodo check: print the graph report plus the planner's
// premature-infeasibility report. It writes nowhere else.
func runCheck(opts *options, strict bool) error {
	_, outcome, _, err := readGraph(opts, configOverrides{})
	if err != nil {
		return &exitError{code: codeError, err: err}
	}

	if opts.json {
		view := checkReport{
			GraphIssues: outcome.Issues,
			Shortfalls:  shortfalls(outcome),
		}
		if err := printJSON(opts.out, view); err != nil {
			return &exitError{code: codeError, err: err}
		}
	} else {
		renderCheckText(opts.out, outcome)
	}

	// --strict: validation failures exit 2. The planner's shortfalls count
	// too: infeasible Tasks are part of check's report (cli.md).
	if n := len(outcome.Issues) + len(outcome.Schedule.Report.Tasks); strict && n > 0 {
		return &exitError{code: codeValidation, err: fmt.Errorf(
			"check --strict: %d graph issue(s), %d infeasible Task(s)",
			len(outcome.Issues), len(outcome.Schedule.Report.Tasks))}
	}
	return nil
}

// runForecast implements lodo forecast: compute and print the Schedule
// that would be written; a dry run with no writes. The calendar read is a
// hard part: resolve (M3), busy time (M4), then the planner — a bad or
// unreachable calendar is an operational error (exit 1), so the forecast
// never prints against a silently free calendar.
func runForecast(opts *options, ov configOverrides, verbose bool) error {
	cfg, client, creds, err := loadGraph(opts, ov)
	if err != nil {
		return &exitError{code: codeError, err: err}
	}

	busy, err := calendarBusy(context.Background(), creds, cfg)
	if err != nil {
		return &exitError{code: codeError, err: err}
	}

	now := time.Now()
	outcome, err := run.Graph(context.Background(), client, busy, cfg, now)
	if err != nil {
		return &exitError{code: codeError, err: err}
	}

	if opts.json {
		view := forecastReport{
			Blocks:     blocks(outcome),
			Shortfalls: shortfalls(outcome),
			Managed:    managedEvents(outcome),
		}
		if verbose {
			view.Explanations = explanations(outcome)
		}
		if err := printJSON(opts.out, view); err != nil {
			return &exitError{code: codeError, err: err}
		}
	} else {
		renderForecastText(opts.out, cfg, outcome, now, verbose)
	}
	return nil
}

// The JSON views are structs only — mapped keys randomise their order,
// structs keep the output byte-stable run to run.

type checkReport struct {
	GraphIssues []resolve.Issue `json:"graph_issues,omitempty"`
	Shortfalls  []shortfallJSON `json:"shortfalls,omitempty"`
}

type forecastReport struct {
	Blocks       []blockJSON       `json:"blocks,omitempty"`
	Shortfalls   []shortfallJSON   `json:"shortfalls,omitempty"`
	Explanations []explanationJSON `json:"explanations,omitempty"` // with --verbose
	// Managed are the calendar's Lodo-made events busy deliberately left
	// out in M4; M5's schedule closes that hole.
	Managed []managedJSON `json:"managed_left_out,omitempty"`
}

// managedJSON is one Managed event run out of the mask, for the note both
// renderings make.
type managedJSON struct {
	ID      string `json:"id"`
	Summary string `json:"summary,omitempty"`
}

type blockJSON struct {
	TaskID string    `json:"task_id"`
	Task   string    `json:"task"`
	Index  int       `json:"index"`
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Slots  int       `json:"slots"`
	Kind   string    `json:"kind"`
}

type shortfallJSON struct {
	TaskID    string `json:"task_id"`
	Task      string `json:"task"`
	Required  int    `json:"required"`
	Placed    int    `json:"placed"`
	StoodDown int    `json:"stood_down"`
	Shortfall int    `json:"shortfall"`
	Reason    string `json:"reason"`
}

type explanationJSON struct {
	TaskID     string    `json:"task_id"`
	BlockIndex int       `json:"block_index"`
	Kind       string    `json:"kind"`
	Start      time.Time `json:"start"`
	Slots      int       `json:"slots"`
	RunStart   time.Time `json:"run_start,omitempty"`
	RunSlots   int       `json:"run_slots,omitempty"`
}

func blocks(outcome run.Outcome) []blockJSON {
	out := make([]blockJSON, 0, len(outcome.Schedule.Blocks))
	for _, b := range outcome.Schedule.Blocks {
		out = append(out, blockJSON{
			TaskID: b.TaskID,
			Task:   run.TaskText(outcome.Tasks, b.TaskID),
			Index:  b.Index,
			Start:  b.Start,
			End:    b.End,
			Slots:  b.Slots,
			Kind:   string(b.Kind),
		})
	}
	return out
}

func shortfalls(outcome run.Outcome) []shortfallJSON {
	out := make([]shortfallJSON, 0, len(outcome.Schedule.Report.Tasks))
	for _, r := range outcome.Schedule.Report.Tasks {
		out = append(out, shortfallJSON{
			TaskID:    r.TaskID,
			Task:      run.TaskText(outcome.Tasks, r.TaskID),
			Required:  r.Required,
			Placed:    r.Placed,
			StoodDown: r.StoodDown,
			Shortfall: r.Shortfall,
			Reason:    string(r.Reason),
		})
	}
	return out
}

func explanations(outcome run.Outcome) []explanationJSON {
	out := make([]explanationJSON, 0, len(outcome.Schedule.Explanations))
	for _, e := range outcome.Schedule.Explanations {
		out = append(out, explanationJSON{
			TaskID:     e.TaskID,
			BlockIndex: e.BlockIndex,
			Kind:       string(e.Kind),
			Start:      e.Start,
			Slots:      e.Slots,
			RunStart:   e.RunStart,
			RunSlots:   e.RunSlots,
		})
	}
	return out
}

// managedEvents is the note's payload: the events busy deliberately left
// out, in view order.
func managedEvents(outcome run.Outcome) []managedJSON {
	out := make([]managedJSON, 0, len(outcome.Managed))
	for _, e := range outcome.Managed {
		out = append(out, managedJSON{ID: e.ID, Summary: e.Summary})
	}
	return out
}

// printJSON writes indented, newline-terminated, deterministic JSON.
func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Text rendering, in the Scheduling timezone the blocks were built in.

func renderCheckText(w io.Writer, outcome run.Outcome) {
	if len(outcome.Issues) == 0 {
		_, _ = fmt.Fprintln(w, "Graph: no issues reported.")
	} else {
		_, _ = fmt.Fprintf(w, "Graph: %d issues\n", len(outcome.Issues))
		for _, issue := range outcome.Issues {
			_, _ = fmt.Fprintf(w, "  [%s] %s: %s\n", issue.Kind, issue.Context, issue.Detail)
		}
	}

	short := outcome.Schedule.Report.Tasks
	if len(short) == 0 {
		_, _ = fmt.Fprintln(w, "Plan (no busy time yet): everything placeable is placed.")
		return
	}
	_, _ = fmt.Fprintf(w, "Plan (no busy time): %d Task(s) could not be fully placed.\n", len(short))
	for _, r := range short {
		_, _ = fmt.Fprintf(w, "  %s: %d of %d slots placed — %s\n",
			taskLabel(outcome, r.TaskID), r.Placed, r.Required, r.Reason)
	}
}

func renderForecastText(w io.Writer, cfg config.Config, outcome run.Outcome, now time.Time, verbose bool) {
	granularity, err := config.ParseGranularity(cfg.Scheduling.Granularity)
	if err != nil {
		granularity = 15 * time.Minute // unreachable after config validation
	}

	loc := schedulingLocation(cfg)
	start := now.In(loc)
	_, _ = fmt.Fprintf(w,
		"Schedule that would be written to calendar %q — %s through %s, %s granularity, %s:\n",
		cfg.Calendar.ID,
		start.Format("Mon 02 Jan 2006"),
		start.AddDate(0, 0, cfg.Scheduling.WindowDays-1).Format("Mon 02 Jan 2006"),
		durationMinutes(granularity, 1),
		loc,
	)

	// The known M4 hole, said plainly: Lodo's own events are not in the
	// busy mask yet, and M5's reconcile is what folds them back in.
	if n := len(outcome.Managed); n > 0 {
		_, _ = fmt.Fprintf(w,
			"\nNote: %d Managed event(s) on the calendar are not counted as busy yet — they are\nthe ones Lodo itself created; the schedule command's reconciliation will account for them:\n", n)
		for _, e := range outcome.Managed {
			summary := e.Summary
			if summary == "" {
				summary = "(no title)"
			}
			_, _ = fmt.Fprintf(w, "  %s — %s\n", e.ID, summary)
		}
	}

	if len(outcome.Schedule.Blocks) == 0 {
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
	for _, b := range outcome.Schedule.Blocks {
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
	sort.Slice(dayRows, func(i, j int) bool { return dayRows[i].day.Before(dayRows[j].day) })

	for _, row := range dayRows {
		_, _ = fmt.Fprintf(w, "\n%s  %s\n", row.when.Format("Mon"), row.day.String())
		for _, b := range row.blocks {
			kind := ""
			if b.Kind == model.BlockPin {
				kind = " (pin)"
			}
			_, _ = fmt.Fprintf(w, "  %s-%s  %s  %s%s\n",
				b.Start.In(loc).Format("15:04"),
				b.End.In(loc).Format("15:04"),
				durationMinutes(granularity, b.Slots),
				taskLabel(outcome, b.TaskID),
				kind,
			)
		}
	}

	if short := outcome.Schedule.Report.Tasks; len(short) > 0 {
		_, _ = fmt.Fprintf(w, "\nShortfall:\n")
		for _, r := range short {
			_, _ = fmt.Fprintf(w, "  %s: %d of %d slots placed — %s\n",
				taskLabel(outcome, r.TaskID), r.Placed, r.Required, r.Reason)
		}
	}

	if verbose {
		_, _ = fmt.Fprintf(w, "\nExplanations:\n")
		for _, e := range outcome.Schedule.Explanations {
			line := fmt.Sprintf("  %s block %d at %s (%d slots), %s",
				taskLabel(outcome, e.TaskID),
				e.BlockIndex,
				e.Start.In(loc).Format("Mon 15:04"),
				e.Slots,
				e.Kind,
			)
			if !e.RunStart.IsZero() {
				line += fmt.Sprintf(" — from the free run starting %s (%d slots)",
					e.RunStart.In(loc).Format("Mon 15:04"), e.RunSlots)
			}
			_, _ = fmt.Fprintln(w, line)
		}
	}
}

// taskLabel prefers the Task's text, falling back to its id.
func taskLabel(outcome run.Outcome, id string) string {
	if text := run.TaskText(outcome.Tasks, id); text != "" {
		return text
	}
	return id
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
