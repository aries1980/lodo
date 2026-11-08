// Package run assembles the loop the CLI commands need: read the graph
// fresh over the HTTP API, resolve it, project the calendar's busy time,
// and plan the Schedule. Schedule and Check fold stored dispositions back
// in through reconciliation; Forecast and ForecastSources stay store-free
// dry runs.
//
// The busy mask is the calendar's explicit input to the planner (ADR 0006):
// resolved tasks plus a mask, never a calendar the planner reaches into.
// Now is injected by the caller, never read here.
package run

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/norm"
	"github.com/aries1980/lodo/internal/planner"
	"github.com/aries1980/lodo/internal/resolve"
)

// BusySource is the calendar part of the loop: busy time already projected
// onto the run's own grid, plus the Managed events deliberately left out of
// it — forecast notes them and schedule's reconcile folds their Blocks
// back. A nil BusySource means no calendar is consulted. The report carries
// the write calendar plus per-read-calendar audit for the busy sources line.
type BusySource interface {
	Busy(ctx context.Context, g *grid.Grid, now time.Time) (busy grid.Mask, managed []calendar.Event, report calendar.BusyReport, err error)
}

// Source is one Logseq graph to read: its client plus identity.
// Name is the graph slug (empty in single-graph mode), Order the config
// index for deterministic cross-graph ordering, DefaultTimeMap the
// graph-level fallback Time Map slug. APIURL is the endpoint the client
// dials; it is carried for diagnostics so an unreachable graph names itself.
type Source struct {
	Name           string
	Order          int
	Client         logseq.Client
	DefaultTimeMap string
	APIURL         string
}

// Scheduled is the core forecast, check, and schedule share: the Tasks that
// were seen, the graph problems to report, the Schedule planned from them,
// and the busy-source audit. Each command's outcome embeds this and adds
// only the fields that command has.
type Scheduled struct {
	Tasks    []model.Task
	Issues   []resolve.Issue
	Schedule planner.Schedule
	// Busy carries the write calendar plus per-read-calendar status for the
	// busy sources line and warnings.
	Busy calendar.BusyReport
}

// ForecastOutcome is one forecast: Scheduled, plus the Managed events busy
// deliberately left out (schedule reconciles them instead) and provenance.
type ForecastOutcome struct {
	Scheduled
	// Managed is the one hole in the forecast mask: Lodo's own events are
	// not folded into busy there; schedule and check reconcile them.
	Managed    []calendar.Event
	Provenance map[string]resolve.TaskProvenance
}

// validateTaskIDs refuses duplicated block UUIDs. Event ids, planner indexes
// and store keys are built from the UUID alone, so two graphs sharing one
// (a cloned graph) would collapse into one Task and one calendar event.
func validateTaskIDs(tasks []model.Task) error {
	seen := make(map[string]model.Task, len(tasks))
	for _, t := range tasks {
		if prev, ok := seen[t.ID]; ok {
			if prev.Graph == t.Graph {
				return fmt.Errorf("task UUID %s appears more than once in %s; block UUIDs must be unique",
					t.ID, graphLabel(t.Graph))
			}
			return fmt.Errorf("task UUID %s appears in %s and %s; cloned graphs share block UUIDs — give the Tasks unique UUIDs before scheduling both",
				t.ID, graphLabel(prev.Graph), graphLabel(t.Graph))
		}
		seen[t.ID] = t
	}
	return nil
}

// graphLabel names a source graph in diagnostics; single-graph mode has no
// slug.
func graphLabel(graph string) string {
	if graph == "" {
		return "single-graph mode"
	}
	return fmt.Sprintf("graph %q", graph)
}

// ReadSources reads every graph and merges the resolved results.
// Time Maps come from the first source only (canonical page); other
// sources contribute Tasks and task issues. Time-Maps issues from
// non-primary sources are dropped — they do not own the page.
// depends:: and order:: chains stay graph-local by construction: each
// graph resolves in isolation, so a cross-graph ref reports
// unknown-dependency and blocks.
func ReadSources(ctx context.Context, sources []Source, timeMapsPage string, loc *time.Location) (resolve.Result, error) {
	if len(sources) == 1 {
		res, err := readOne(ctx, sources[0].Client, timeMapsPage, loc, sources[0].Name, sources[0].Order, sources[0].DefaultTimeMap)
		if err != nil {
			return resolve.Result{}, err
		}
		if err := validateTaskIDs(res.Tasks); err != nil {
			return resolve.Result{}, err
		}
		return res, nil
	}
	var allTasks []model.Task
	var allIssues []resolve.Issue
	var maps []model.TimeMap
	provenance := map[string]resolve.TaskProvenance{}
	first := true
	for _, s := range sources {
		res, err := readOne(ctx, s.Client, timeMapsPage, loc, s.Name, s.Order, s.DefaultTimeMap)
		if err != nil {
			return resolve.Result{}, fmt.Errorf("graph %s: %w", s.Name, err)
		}
		allTasks = append(allTasks, res.Tasks...)
		for id, p := range res.Provenance {
			provenance[id] = p
		}
		if first {
			maps = res.TimeMaps
			allIssues = append(allIssues, res.Issues...)
			first = false
			continue
		}
		for _, is := range res.Issues {
			switch is.Kind {
			case resolve.IssueMissingTimeMapsPage, resolve.IssueDefaultMapCount,
				resolve.IssueDuplicateMap, resolve.IssueBadWindow,
				resolve.IssueBadTimeMap, resolve.IssueBadDefault:
				continue
			}
			allIssues = append(allIssues, is)
		}
	}
	if err := validateTaskIDs(allTasks); err != nil {
		return resolve.Result{}, err
	}
	return resolve.Result{Tasks: allTasks, TimeMaps: maps, Issues: allIssues, Provenance: provenance}, nil
}

// readOne walks one graph the way logseq-integration.md specifies: enumerate
// pages, fetch each page's block tree, and the page properties wherever they
// can matter (task-marked blocks inherit from their page, and the Time Maps
// page needs its tree even when nothing on it is TODO).
func readOne(ctx context.Context, client logseq.Client, timeMapsPage string, loc *time.Location, graph string, order int, defaultMap string) (resolve.Result, error) {
	pages, err := client.Pages(ctx)
	if err != nil {
		return resolve.Result{}, err
	}
	sortPages(pages)

	tmName := lowerTrim(timeMapsPage)
	input := resolve.Input{
		Graph:          graph,
		GraphOrder:     order,
		DefaultTimeMap: defaultMap,
		Pages:          pages,
		Trees:          make(map[string][]*logseq.Block, len(pages)),
		TimeMapsPage:   timeMapsPage,
		Location:       loc,
	}

	for _, p := range pages {
		tree, err := client.Tree(ctx, p.Name)
		if err != nil {
			return resolve.Result{}, err
		}
		input.Trees[p.Name] = tree

		if lowerTrim(p.Name) != tmName && !hasCandidate(tree) {
			continue // page properties cannot matter for a page with no candidates
		}
		info, err := client.Page(ctx, p.Name)
		if err != nil {
			return resolve.Result{}, err
		}
		if info == nil {
			continue // the page vanished between enumeration and the fetch
		}
		p.Properties = info.Properties
	}

	return resolve.Resolve(input)
}

// Forecast reads one graph and plans the Schedule without touching the
// store. It is the single-graph convenience wrapper around ForecastSources.
func Forecast(ctx context.Context, client logseq.Client, busy BusySource, cfg config.Config, now time.Time) (ForecastOutcome, error) {
	return ForecastSources(ctx, []Source{{Client: client}}, busy, cfg, now)
}

// ForecastSources reads every source, projects the calendar's busy time, and
// plans the Schedule. The forecast command and schedule --dry-run call this;
// it never touches the store.
func ForecastSources(ctx context.Context, sources []Source, busy BusySource, cfg config.Config, now time.Time) (ForecastOutcome, error) {
	granularity, err := config.ParseGranularity(cfg.Scheduling.Granularity)
	if err != nil {
		return ForecastOutcome{}, err
	}
	location, err := cfg.Scheduling.Location()
	if err != nil {
		return ForecastOutcome{}, err
	}
	result, err := ReadSources(ctx, sources, cfg.Logseq.PrimaryTimeMapsPage(), location)
	if err != nil {
		return ForecastOutcome{}, err
	}

	var inputBusy grid.Mask
	var managed []calendar.Event
	var busyReport calendar.BusyReport
	if busy != nil {
		g := grid.Build(now, location, granularity, cfg.Scheduling.WindowDays)
		inputBusy, managed, busyReport, err = busy.Busy(ctx, g, now)
		if err != nil {
			return ForecastOutcome{}, err
		}
	}

	schedule, err := planner.Plan(planner.Input{
		Tasks:        result.Tasks,
		TimeMaps:     result.TimeMaps,
		Busy:         inputBusy,
		Dispositions: nil, // forecast stays store-free by design
		Now:          now,
		Granularity:  granularity,
		WindowDays:   cfg.Scheduling.WindowDays,
		Location:     location,
	})
	if err != nil {
		return ForecastOutcome{}, err
	}

	return ForecastOutcome{
		Scheduled: Scheduled{
			Tasks: result.Tasks, Issues: result.Issues, Schedule: schedule, Busy: busyReport,
		},
		Managed: managed, Provenance: result.Provenance,
	}, nil
}

// hasCandidate reports whether any block in the tree is marked TODO or
// DOING — exactly the gate halves (ADR 0004) a page can pass on via its
// properties.
func hasCandidate(tree []*logseq.Block) bool {
	found := false
	if err := logseq.Walk(tree, func(b *logseq.Block) error {
		switch strings.ToUpper(strings.TrimSpace(b.Marker)) {
		case "TODO", "DOING":
			found = true
		}
		return nil
	}); err != nil {
		return false // unreachable: the visitor never errors
	}
	return found
}

// lowerTrim compares page names the way Logseq does: case-insensitively.
func lowerTrim(s string) string {
	return norm.ID(s)
}

// sortPages fixes the enumeration order lexicographically so outline order
// never depends on the app's internal page order.
func sortPages(pages []*logseq.Page) {
	slices.SortFunc(pages, func(a, b *logseq.Page) int {
		if c := strings.Compare(norm.ID(a.Name), norm.ID(b.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
}
