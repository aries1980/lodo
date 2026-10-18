// Package run assembles the loop the CLI commands need: read the graph
// fresh over the HTTP API, resolve it, project the calendar's busy time,
// and plan the Schedule. Schedule and Check fold stored dispositions back
// in through reconciliation; Graph (the forecast path) stays a store-free
// dry run.
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
// graph-level fallback Time Map slug.
type Source struct {
	Name           string
	Order          int
	Client         logseq.Client
	DefaultTimeMap string
}

// Outcome is one forecast's read side: the Tasks that were seen, the graph
// problems to report, the Schedule planned from them, and the Managed
// events busy deliberately left out (schedule reconciles them instead).
type Outcome struct {
	Tasks    []model.Task
	Issues   []resolve.Issue
	Schedule planner.Schedule
	// Managed is the one hole in the forecast mask: Lodo's own events are
	// not folded into busy there; schedule and check reconcile them.
	Managed []calendar.Event
	// Busy carries the write calendar plus per-read-calendar status for the
	// busy sources line and warnings.
	Busy calendar.BusyReport
}

// Graph reads the graph fresh, resolves it, projects the calendar's busy
// time and plans the Schedule with the effective config's settings.
// Forecast and schedule --dry-run call this; it never touches the store.
func Graph(ctx context.Context, client logseq.Client, busy BusySource, cfg config.Config, now time.Time) (Outcome, error) {
	granularity, err := config.ParseGranularity(cfg.Scheduling.Granularity)
	if err != nil {
		return Outcome{}, err
	}
	location, err := cfg.Scheduling.Location()
	if err != nil {
		return Outcome{}, err
	}

	result, err := Read(ctx, client, cfg.Logseq.TimeMapsPage, location)
	if err != nil {
		return Outcome{}, err
	}

	var inputBusy grid.Mask
	var managed []calendar.Event
	var busyReport calendar.BusyReport
	if busy != nil {
		g := grid.Build(now, location, granularity, cfg.Scheduling.WindowDays)
		inputBusy, managed, busyReport, err = busy.Busy(ctx, g, now)
		if err != nil {
			return Outcome{}, err
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
		return Outcome{}, err
	}

	return Outcome{Tasks: result.Tasks, Issues: result.Issues, Schedule: schedule, Managed: managed, Busy: busyReport}, nil
}

// Read walks the read strategy from logseq-integration.md: enumerate pages,
// then fetch each page's block tree, and the page properties wherever they
// can matter (task-marked blocks inherit from their page, and the Time Maps
// page needs its tree even when nothing on it is TODO).
func Read(ctx context.Context, client logseq.Client, timeMapsPage string, loc *time.Location) (resolve.Result, error) {
	pages, err := client.Pages(ctx)
	if err != nil {
		return resolve.Result{}, err
	}
	sortPages(pages)

	tmName := lowerTrim(timeMapsPage)
	input := resolve.Input{
		Pages:        pages,
		Trees:        make(map[string][]*logseq.Block, len(pages)),
		TimeMapsPage: timeMapsPage,
		Location:     loc,
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
		return res, nil
	}
	var allTasks []model.Task
	var allIssues []resolve.Issue
	var maps []model.TimeMap
	first := true
	for _, s := range sources {
		res, err := readOne(ctx, s.Client, timeMapsPage, loc, s.Name, s.Order, s.DefaultTimeMap)
		if err != nil {
			return resolve.Result{}, fmt.Errorf("graph %s: %w", s.Name, err)
		}
		allTasks = append(allTasks, res.Tasks...)
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
	return resolve.Result{Tasks: allTasks, TimeMaps: maps, Issues: allIssues}, nil
}

// readOne is Read plus graph identity.
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

// GraphSources is Graph over multiple graphs.
func GraphSources(ctx context.Context, sources []Source, busy BusySource, cfg config.Config, now time.Time) (Outcome, error) {
	granularity, err := config.ParseGranularity(cfg.Scheduling.Granularity)
	if err != nil {
		return Outcome{}, err
	}
	location, err := cfg.Scheduling.Location()
	if err != nil {
		return Outcome{}, err
	}
	result, err := ReadSources(ctx, sources, cfg.Logseq.PrimaryTimeMapsPage(), location)
	if err != nil {
		return Outcome{}, err
	}

	var inputBusy grid.Mask
	var managed []calendar.Event
	var busyReport calendar.BusyReport
	if busy != nil {
		g := grid.Build(now, location, granularity, cfg.Scheduling.WindowDays)
		inputBusy, managed, busyReport, err = busy.Busy(ctx, g, now)
		if err != nil {
			return Outcome{}, err
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
		return Outcome{}, err
	}

	return Outcome{Tasks: result.Tasks, Issues: result.Issues, Schedule: schedule, Managed: managed, Busy: busyReport}, nil
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

// TextMap indexes Task display text by ID for O(1) rendering lookups.
func TextMap(tasks []model.Task) map[string]string {
	out := make(map[string]string, len(tasks))
	for _, t := range tasks {
		out[t.ID] = t.Text
	}
	return out
}

// GraphMap indexes Task graph by ID.
func GraphMap(tasks []model.Task) map[string]string {
	out := make(map[string]string, len(tasks))
	for _, t := range tasks {
		if _, ok := out[t.ID]; !ok {
			out[t.ID] = t.Graph
		}
	}
	return out
}

// Label prefers the Task's text, falling back to its id.
func Label(tasks []model.Task, id string) string {
	return LabelWithMap(TextMap(tasks), id)
}

// LabelWithMap prefers the mapped text, falling back to the id.
func LabelWithMap(byID map[string]string, id string) string {
	if text, ok := byID[id]; ok && text != "" {
		return text
	}
	return id
}

// TaskText returns the display text of the Task with the given id, for
// check and forecast rows. Empty when the id is unknown.
func TaskText(tasks []model.Task, id string) string {
	for _, t := range tasks {
		if t.ID == id {
			return t.Text
		}
	}
	return ""
}
