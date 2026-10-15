// Package run assembles the read side of the loop the CLI commands need:
// read the graph fresh over the HTTP API, resolve it, project the
// calendar's busy time, and plan the Schedule.
//
// The busy mask is the calendar's explicit input to the planner (ADR 0006):
// resolved tasks plus a mask, never a calendar the planner reaches into.
// Dispositions stay empty — the store is M5's work — and Now is injected by
// the caller, never read here.
package run

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/planner"
	"github.com/aries1980/lodo/internal/resolve"
)

// BusySource is the calendar part of the loop: busy time already projected
// onto the run's own grid, plus the Managed events that were deliberately
// left out of it — no dispositions exist yet, so M4 notes them and M5's
// reconcile folds their Blocks back. A nil BusySource means no calendar is
// consulted; `check` never consults one.
type BusySource interface {
	Busy(ctx context.Context, g *grid.Grid, now time.Time) (busy grid.Mask, managed []calendar.Event, err error)
}

// Outcome is one command's read side: the Tasks that were seen, the graph
// problems to report, the Schedule planned from them, and the Managed
// events busy deliberately left out.
type Outcome struct {
	Tasks    []model.Task
	Issues   []resolve.Issue
	Schedule planner.Schedule
	// Managed is the one hole in the mask M4 leaves: Lodo's own events are
	// not folded into busy until reconciliation exists.
	Managed []calendar.Event
}

// Graph reads the graph fresh, resolves it, projects the calendar's busy
// time and plans the Schedule with the effective config's settings. Both
// check and forecast call this; writes start in M5.
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
	if busy != nil {
		g := grid.Build(now, location, granularity, cfg.Scheduling.WindowDays)
		inputBusy, managed, err = busy.Busy(ctx, g, now)
		if err != nil {
			return Outcome{}, err
		}
	}

	schedule, err := planner.Plan(planner.Input{
		Tasks:        result.Tasks,
		TimeMaps:     result.TimeMaps,
		Busy:         inputBusy,
		Dispositions: nil, // M4: the store does not exist yet
		Now:          now,
		Granularity:  granularity,
		WindowDays:   cfg.Scheduling.WindowDays,
		Location:     location,
	})
	if err != nil {
		return Outcome{}, err
	}

	return Outcome{Tasks: result.Tasks, Issues: result.Issues, Schedule: schedule, Managed: managed}, nil
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
	return strings.ToLower(strings.TrimSpace(s))
}

// sortPages fixes the enumeration order lexicographically so outline order
// never depends on the app's internal page order.
func sortPages(pages []*logseq.Page) {
	sort.Slice(pages, func(i, j int) bool {
		a, b := lowerTrim(pages[i].Name), lowerTrim(pages[j].Name)
		if a != b {
			return a < b
		}
		return pages[i].Name < pages[j].Name
	})
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
