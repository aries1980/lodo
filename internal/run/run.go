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
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/graph"
	"github.com/aries1980/lodo/internal/grid"
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

// Source is one task backend to read: its client plus identity.
// Name is the source slug (empty in single-source mode), Order the config
// index for deterministic cross-source ordering, DefaultTimeMap the
// source-level fallback Time Map slug. APIURL is the endpoint the client
// dials; it is carried for diagnostics so an unreachable source names
// itself. Backend names the adapter ("logseq", "keep-official",
// "keep-unofficial", …) for diagnostics and probe messages.
type Source struct {
	Name           string
	Order          int
	Client         graph.Client
	DefaultTimeMap string
	APIURL         string
	Backend        string
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
// Time-map slugs in every graph validate against the canonical maps, so a
// secondary task naming a primary map is not falsely reported.
// depends:: and order:: chains stay graph-local by construction: each
// graph resolves in isolation, so a cross-graph ref reports
// unknown-dependency and blocks.
func ReadSources(ctx context.Context, sources []Source, timeMapsPage string, loc *time.Location) (resolve.Result, error) {
	if len(sources) == 1 {
		res, _, _, _, err := LoadGraph(ctx, sources[0], timeMapsPage, loc)
		if err != nil {
			return resolve.Result{}, err
		}
		if err := ValidateTaskIDs(res.Tasks); err != nil {
			return resolve.Result{}, err
		}
		res.Issues = RevalidateTimeMaps(res.Tasks, res.TimeMaps, timeMapsPage, res.Issues)
		return res, nil
	}
	var allTasks []model.Task
	var allIssues []resolve.Issue
	var maps []model.TimeMap
	provenance := map[string]resolve.TaskProvenance{}
	first := true
	for _, s := range sources {
		res, _, _, _, err := LoadGraph(ctx, s, timeMapsPage, loc)
		if err != nil {
			if len(sources) > 1 {
				return resolve.Result{}, fmt.Errorf("graph %s: %w", s.Name, err)
			}
			return resolve.Result{}, err
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
	if err := ValidateTaskIDs(allTasks); err != nil {
		return resolve.Result{}, err
	}
	allIssues = RevalidateTimeMaps(allTasks, maps, timeMapsPage, allIssues)
	return resolve.Result{Tasks: allTasks, TimeMaps: maps, Issues: allIssues, Provenance: provenance}, nil
}

// Forecast reads one graph and plans the Schedule without touching the
// store. It is the single-graph convenience wrapper around ForecastSources.
func Forecast(ctx context.Context, client graph.Client, busy BusySource, cfg config.Config, now time.Time) (ForecastOutcome, error) {
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
func hasCandidate(tree []*graph.Block) bool {
	found := false
	if err := graph.Walk(tree, func(b *graph.Block) error {
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
func sortPages(pages []*graph.Page) {
	slices.SortFunc(pages, func(a, b *graph.Page) int {
		return cmp.Or(cmp.Compare(norm.ID(a.Name), norm.ID(b.Name)), cmp.Compare(a.Name, b.Name))
	})
}

// validatePageNames refuses duplicate page names within one source,
// compared case-insensitively like page names everywhere else. The
// resolver indexes trees by name, so two pages sharing one would collapse
// into a single tree; id-addressed reads (ADR 0026) mean no backend title
// lookup guards this anymore, so the caller does, once, for every backend.
func validatePageNames(source string, pages []*graph.Page) error {
	counts := make(map[string]int, len(pages))
	for _, p := range pages {
		if key := norm.ID(p.Name); key != "" {
			counts[key]++
		}
	}
	for _, p := range pages {
		if counts[norm.ID(p.Name)] > 1 {
			return fmt.Errorf("%s: %d pages named %q; page names must be unique within a source",
				graphLabel(source), counts[norm.ID(p.Name)], p.Name)
		}
	}
	return nil
}
