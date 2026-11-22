// Shared graph loading: one Logseq graph read plus canonical Time Maps
// validation. Both run.ReadSources and tui.Load build on LoadGraph so the
// read strategy, duplicate detection, and secondary-graph Time Maps
// validation cannot drift.
package run

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/aries1980/lodo/internal/fanout"
	"github.com/aries1980/lodo/internal/graph"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/resolve"
	"github.com/aries1980/lodo/internal/set"
)

// readWorkers bounds concurrent page reads. The app serves one graph per
// window; four keeps it responsive while cutting the serial round-trips that
// dominated a first run (ADR 0024).
const readWorkers = 4

// ValidateTaskIDs refuses duplicated block UUIDs. Event ids, planner indexes
// and store keys are built from the UUID alone, so two graphs sharing one
// (a cloned graph) would collapse into one Task and one calendar event.
func ValidateTaskIDs(tasks []model.Task) error {
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

// pageRead is one page's concurrent read: its block tree and, when the page
// can pass on properties, its page info.
type pageRead struct {
	tree []*graph.Block
	info *graph.Page
}

// LoadGraph reads one graph: enumerate pages, fetch each page's block tree
// and the page properties wherever they can matter, resolve it, and index
// blocks by UUID for TUI editing. It returns the resolved result, the resolve
// input (for TUI revalidation), the block index, and page names by block.
//
// Page trees are fetched concurrently with a bounded fan-out. The tree map and
// page properties are assembled in page order after the reads, so the result
// matches a serial read byte for byte.
func LoadGraph(ctx context.Context, source Source, timeMapsPage string, loc *time.Location) (resolve.Result, resolve.Input, map[string]*graph.Block, map[string]string, error) {
	pages, err := source.Client.Pages(ctx)
	if err != nil {
		return resolve.Result{}, resolve.Input{}, nil, nil, err
	}
	sortPages(pages)
	if err := validatePageNames(source.Name, pages); err != nil {
		return resolve.Result{}, resolve.Input{}, nil, nil, err
	}

	tmName := lowerTrim(timeMapsPage)
	input := resolve.Input{
		Graph:          source.Name,
		GraphOrder:     source.Order,
		DefaultTimeMap: source.DefaultTimeMap,
		Pages:          pages,
		Trees:          make(map[string][]*graph.Block, len(pages)),
		TimeMapsPage:   timeMapsPage,
		Location:       loc,
	}
	reads, err := fanout.Do(ctx, len(pages), readWorkers, func(ctx context.Context, i int) (pageRead, error) {
		p := pages[i]
		tree, err := source.Client.Tree(ctx, p.UUID, p.Name)
		if err != nil {
			return pageRead{}, err
		}
		read := pageRead{tree: tree}
		if lowerTrim(p.Name) != tmName && !hasCandidate(tree) {
			return read, nil
		}
		info, err := source.Client.Page(ctx, p.UUID, p.Name)
		if err != nil {
			return pageRead{}, err
		}
		read.info = info
		return read, nil
	})
	if err != nil {
		return resolve.Result{}, resolve.Input{}, nil, nil, err
	}
	for i, p := range pages {
		input.Trees[p.Name] = reads[i].tree
		if reads[i].info != nil {
			p.Properties = reads[i].info.Properties
		}
	}
	res, err := resolve.Resolve(input)
	if err != nil {
		return resolve.Result{}, resolve.Input{}, nil, nil, err
	}
	blocks := make(map[string]*graph.Block, len(res.Tasks))
	pageOf := make(map[string]string, len(res.Tasks))
	for _, p := range pages {
		tree := input.Trees[p.Name]
		_ = graph.Walk(tree, func(b *graph.Block) error {
			if b.UUID == "" {
				return nil
			}
			if _, ok := blocks[b.UUID]; !ok {
				blocks[b.UUID] = b
				pageOf[b.UUID] = p.Name
			}
			return nil
		})
	}
	return res, input, blocks, pageOf, nil
}

// RevalidateTimeMaps validates every task's time-map slugs against the
// canonical maps (the primary graph's page). Secondary graphs resolve in
// isolation, so their own Time Maps page would otherwise validate tasks
// against maps the planner never sees. Unknown-time-map issues are rebuilt
// from scratch; all other issues pass through untouched.
func RevalidateTimeMaps(tasks []model.Task, maps []model.TimeMap, timeMapsPage string, issues []resolve.Issue) []resolve.Issue {
	canonical := make(set.Set[string], len(maps))
	for _, m := range maps {
		canonical.Add(m.Slug)
	}
	kept := make([]resolve.Issue, 0, len(issues))
	for _, is := range issues {
		if is.Kind != resolve.IssueUnknownTimeMap {
			kept = append(kept, is)
		}
	}
	for _, t := range tasks {
		seen := make(set.Set[string], len(t.TimeMaps))
		for _, slug := range t.TimeMaps {
			if !seen.Add(slug) {
				continue
			}
			if !canonical.Has(slug) {
				kept = append(kept, resolve.Issue{
					Kind:    resolve.IssueUnknownTimeMap,
					Context: taskContext(t),
					Detail:  fmt.Sprintf("time-map:: %s references no Time Map defined on %q", slug, timeMapsPage),
				})
			}
		}
	}
	slices.SortFunc(kept, func(a, b resolve.Issue) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Context, b.Context), cmp.Compare(a.Detail, b.Detail))
	})
	return kept
}

func taskContext(t model.Task) string {
	if t.Graph != "" {
		return fmt.Sprintf("graph %s, task %s", t.Graph, t.ID)
	}
	return fmt.Sprintf("task %s", t.ID)
}
