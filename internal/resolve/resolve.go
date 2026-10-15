package resolve

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/model"
)

// Input is the raw graph as read from the app plus how to interpret it,
// after the read strategy in logseq-integration.md: every page, the trees
// to walk, and the page the Time Maps live on.
type Input struct {
	// Pages enumerates the graph's pages; Properties may carry inheritable
	// defaults for the blocks on the page.
	Pages []*logseq.Page
	// Trees maps a page name (spelled as in Pages) to its block tree.
	Trees map[string][]*logseq.Block
	// TimeMapsPage is the configured page name Time Maps are authored on
	// (logseq.time_maps_page, default "Time Maps").
	TimeMapsPage string
	// Location is the scheduling timezone: Pins and org stamps resolve in
	// it.
	Location *time.Location
}

// Result is the resolved graph: everything the planner is fed (ADR 0006)
// plus the graph validation report check prints.
type Result struct {
	Tasks    []model.Task
	TimeMaps []model.TimeMap
	Issues   []Issue
}

// Resolve turns payloads into Tasks, Time Maps and a graph validation
// report. Everything the parser owns is consumed, not reimplemented —
// ParseDuration for the gate, ParseOrgStamp and ParseCivilDate for the
// stamps, ParsePriority, ParseWindow, Slugify — and anything unparseable
// becomes a reported Issue, never a guess.
func Resolve(in Input) (Result, error) {
	if in.Location == nil {
		return Result{}, fmt.Errorf("resolve: location is required")
	}

	r := &resolver{in: in, mapSlugs: map[string]bool{}, pageProps: map[string]string{}}
	r.timeMaps()
	r.resolveTasks()
	return Result{Tasks: r.tasks, TimeMaps: r.maps, Issues: r.issues}, nil
}

// resolver walks one Input, collecting the output.
type resolver struct {
	in Input

	issues    []Issue
	maps      []model.TimeMap
	mapSlugs  map[string]bool // slug → a map defines it
	tasks     []model.Task
	pageProps map[string]string // page\x00key → inherited default text
}

// issue appends one reported problem. The context stays plain ("page 2026-10-15, block …");
// the check renderer inserts the separator.
func (r *resolver) issue(kind, context, detail string) {
	r.issues = append(r.issues, Issue{Kind: kind, Context: context, Detail: detail})
}

// timeMapsIndex is the position of the Time Maps page in Pages, -1 when the
// graph does not have it.
func (r *resolver) timeMapsIndex() int {
	return r.pageIndex(r.in.TimeMapsPage)
}

// pageIndex matches a page by name case-insensitively (Logseq page names
// are case-insensitive), -1 when absent.
func (r *resolver) pageIndex(name string) int {
	want := strings.ToLower(strings.TrimSpace(name))
	for i, p := range r.in.Pages {
		if strings.ToLower(strings.TrimSpace(p.Name)) == want {
			return i
		}
	}
	return -1
}

// treeFor finds the block tree of a case-insensitive page name.
func (r *resolver) treeFor(name string) []*logseq.Block {
	want := strings.ToLower(strings.TrimSpace(name))
	for key, tree := range r.in.Trees {
		if strings.ToLower(strings.TrimSpace(key)) == want {
			return tree
		}
	}
	return nil
}

// sortedPages orders pages so the walk is deterministic, whichever order
// the app enumerated them in.
func (r *resolver) sortedPages() []*logseq.Page {
	pages := make([]*logseq.Page, len(r.in.Pages))
	copy(pages, r.in.Pages)
	sort.Slice(pages, func(i, j int) bool {
		a, b := pages[i].Name, pages[j].Name
		la, lb := strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b))
		if la != lb {
			return la < lb
		}
		return a < b
	})
	return pages
}

// blockContext locates a block for an Issue: the page and the block UUID.
func (r *resolver) blockContext(page *logseq.Page, b *logseq.Block) string {
	return fmt.Sprintf("page %s, block %s", page.Name, b.UUID)
}

// tmBlockContext locates a block on the Time Maps page.
func (r *resolver) tmBlockContext(b *logseq.Block) string {
	return fmt.Sprintf("page %s, block %s", r.in.TimeMapsPage, b.UUID)
}

// taskContext locates a Task for an Issue.
func taskContext(t model.Task) string {
	return fmt.Sprintf("task %s", t.ID)
}
