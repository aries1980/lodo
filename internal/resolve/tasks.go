package resolve

import (
	"fmt"
	"slices"
	"strings"

	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/norm"
	"github.com/aries1980/lodo/internal/parse"
)

// tasks resolves candidate Tasks from every page but the Time Maps page: a
// block is a Task iff it is TODO/DOING and carries an explicit duration::
// (ADR 0004); anything else is a note, skipped — and reported when the
// omission would otherwise be silent.
//
// Nesting (SkedPal parity): a Task with Task descendants is a container,
// never scheduled itself; DONE/CANCELLED ancestors suppress their subtree;
// order:: sequential chains leaf Tasks in outline order; depends:: adds
// explicit finish-to-start edges.
func (r *resolver) resolveTasks() {
	for _, page := range r.sortedPages() {
		if page == nil || page.Properties == nil {
			continue
		}
		pageProps := normPageProps(page)
		for _, key := range []string{"time-map", "priority", "plan-start", "plan-end", "order"} {
			raw, ok := pageProps[key]
			if !ok {
				continue
			}
			text, okT := propsText(raw)
			if !okT {
				r.issue(IssueBadProperty,
					r.pageContext(page.Name),
					fmt.Sprintf("%s:: has a malformed value the app parsed as %v; the page-level default is skipped", key, raw))
				continue
			}
			r.pageDefault(page.Name, key, text)
		}
	}

	type candidate struct {
		page      *logseq.Page
		block     *logseq.Block
		ancestors []*logseq.Block // root-first stack of parent blocks
	}
	var cands []candidate
	doneSet := map[string]bool{} // UUID (lower) -> DONE/CANCELLED block
	// Collect every block with its ancestor stack for order chaining.
	type everyBlock struct {
		page      *logseq.Page
		block     *logseq.Block
		ancestors []*logseq.Block
	}
	var all []everyBlock

	tmIdx := r.timeMapsIndex()
	for _, page := range r.sortedPages() {
		if idx := r.pageIndex(page.Name); idx == tmIdx {
			continue // the Time Maps page defines maps, never Tasks
		}
		tree := r.treeFor(page.Name)
		if tree == nil {
			continue
		}
		page := page
		var walk func(blocks []*logseq.Block, ancestors []*logseq.Block)
		walk = func(blocks []*logseq.Block, ancestors []*logseq.Block) {
			for _, b := range blocks {
				if b == nil {
					continue
				}
				all = append(all, everyBlock{page: page, block: b, ancestors: append([]*logseq.Block(nil), ancestors...)})
				marker := model.Marker(strings.ToUpper(strings.TrimSpace(b.Marker)))
				if marker == model.MarkerDONE || marker == model.MarkerCANCELLED {
					if id := norm.ID(b.UUID); id != "" {
						doneSet[id] = true
					}
				}
				// Suppression: any DONE/CANCELLED ancestor hides the subtree.
				suppressed := false
				for _, a := range ancestors {
					am := model.Marker(strings.ToUpper(strings.TrimSpace(a.Marker)))
					if am == model.MarkerDONE || am == model.MarkerCANCELLED {
						suppressed = true
						break
					}
				}
				switch marker {
				case model.MarkerTODO, model.MarkerDOING:
					if !suppressed {
						cands = append(cands, candidate{page: page, block: b, ancestors: append([]*logseq.Block(nil), ancestors...)})
					}
				}
				walk(b.Children, append(ancestors, b))
			}
		}
		walk(tree, nil)
	}

	// Resolve each candidate in outline order (pages sorted, depth-first).
	// OutlineOrder counts TODO/DOING candidates as before.
	order := 0
	type resolved struct {
		task      model.Task
		page      *logseq.Page
		block     *logseq.Block
		ancestors []*logseq.Block
		explicit  []string // explicit depends (raw validated, before container expansion)
	}
	var res []resolved
	for _, c := range cands {
		t, isTask, issue := r.taskWithAncestors(c.page, c.block, c.ancestors)
		if issue != nil {
			r.issue(issue.Kind, issue.Context, issue.Detail)
		}
		if isTask {
			t.OutlineOrder = order
			res = append(res, resolved{task: t, page: c.page, block: c.block, ancestors: c.ancestors, explicit: append([]string(nil), t.Depends...)})
		}
		order++
	}

	// Index tasks by lower UUID.
	byID := map[string]int{}
	for i, x := range res {
		byID[norm.ID(x.task.ID)] = i
	}
	// Descendant map: task index -> descendant task indices.
	// For each task, walk its ancestors once and link to the ancestor
	// tasks directly: O(n*depth) instead of O(n²).
	descendants := make(map[int][]int)
	for i, x := range res {
		seen := map[int]bool{}
		for _, a := range x.ancestors {
			id := norm.ID(a.UUID)
			if id == "" {
				continue
			}
			if j, ok := byID[id]; ok && j != i && !seen[j] {
				seen[j] = true
				descendants[j] = append(descendants[j], i)
			}
		}
	}
	isContainer := map[int]bool{}
	for j, ds := range descendants {
		if len(ds) > 0 {
			isContainer[j] = true
		}
	}
	// ParentID: nearest ancestor that is a Task.
	for i, x := range res {
		for k := len(x.ancestors) - 1; k >= 0; k-- {
			id := norm.ID(x.ancestors[k].UUID)
			if j, ok := byID[id]; ok {
				res[i].task.ParentID = res[j].task.ID
				break
			}
		}
	}
	for i := range res {
		if isContainer[i] {
			res[i].task.Container = true
		}
	}

	// Helper: leaves of a task (transitive leaf Tasks).
	leavesOf := func(j int) []int {
		if !isContainer[j] {
			return []int{j}
		}
		var out []int
		for _, d := range descendants[j] {
			if !isContainer[d] {
				out = append(out, d)
			}
		}
		slices.SortFunc(out, func(a, b int) int { return res[a].task.OutlineOrder - res[b].task.OutlineOrder })
		return out
	}

	// Validate explicit depends: DONE/CANCELLED drops, unknown kept+reported.
	for i := range res {
		var kept []string
		for _, dep := range res[i].explicit {
			lower := norm.ID(dep)
			if lower == "" {
				continue
			}
			if strings.HasPrefix(lower, "malformed-depends:") {
				kept = append(kept, lower)
				continue
			}
			if lower == norm.ID(res[i].task.ID) {
				r.issue(IssueDependencyCycle, taskContext(res[i].task),
					fmt.Sprintf("depends:: references itself ((%s)); the Task is left unscheduled", res[i].task.ID))
				kept = append(kept, res[i].task.ID)
				continue
			}
			if j, ok := byID[lower]; ok {
				if isContainer[j] {
					for _, leaf := range leavesOf(j) {
						kept = append(kept, res[leaf].task.ID)
					}
				} else {
					kept = append(kept, res[j].task.ID)
				}
				continue
			}
			if doneSet[lower] {
				continue // DONE/CANCELLED satisfies.
			}
			r.issue(IssueUnknownDependency, taskContext(res[i].task),
				fmt.Sprintf("depends:: ((%s)) references no Task and no DONE/CANCELLED block; the dependent is blocked until it is fixed", dep))
			kept = append(kept, dep)
		}
		res[i].explicit = kept
	}

	// Sequential chaining: for every block (and page) with effective
	// order==sequential, chain descendant leaf Tasks in outline order.
	// Effective order for a block: block explicit > ancestors > page > parallel.
	// Explicit orders are parsed once and cached; descendant leaves are
	// indexed once by ancestor block ID so each sequential block is a map
	// lookup instead of a full res scan.
	explicitCache := make(map[*logseq.Block]model.OrderMode)
	hasExplicit := make(map[*logseq.Block]bool)
	explicitOrder := func(b *logseq.Block) (model.OrderMode, bool) {
		if b == nil {
			return "", false
		}
		if v, ok := hasExplicit[b]; ok {
			return explicitCache[b], v
		}
		m, ok := parseExplicitOrder(b)
		explicitCache[b] = m
		hasExplicit[b] = ok
		return m, ok
	}
	effectiveOrder := func(page *logseq.Page, ancestors []*logseq.Block, b *logseq.Block) model.OrderMode {
		if m, ok := explicitOrder(b); ok {
			return m
		}
		for k := len(ancestors) - 1; k >= 0; k-- {
			if m, ok := explicitOrder(ancestors[k]); ok {
				return m
			}
		}
		if page != nil {
			if text := r.pageDefault(page.Name, "order", ""); text != "" {
				if m, err := parse.ParseOrder(text); err == nil {
					return m
				}
			}
		}
		return model.OrderParallel
	}
	// Ancestor block ID -> descendant leaf task indices (in res order,
	// which is outline order).
	leavesByAncestor := make(map[string][]int)
	for idx, x := range res {
		if isContainer[idx] {
			continue
		}
		for _, a := range x.ancestors {
			id := norm.ID(a.UUID)
			if id == "" {
				continue
			}
			leavesByAncestor[id] = append(leavesByAncestor[id], idx)
		}
	}
	// Map block UUID -> task index for leaves lookup.
	// For each block in all, if sequential, find descendant leaf tasks.
	for _, eb := range all {
		if effectiveOrder(eb.page, eb.ancestors, eb.block) != model.OrderSequential {
			continue
		}
		id := norm.ID(eb.block.UUID)
		leaves := append([]int(nil), leavesByAncestor[id]...)
		slices.SortFunc(leaves, func(a, b int) int { return res[a].task.OutlineOrder - res[b].task.OutlineOrder })
		for k := 1; k < len(leaves); k++ {
			prev := res[leaves[k-1]].task.ID
			cur := leaves[k]
			if !slices.Contains(res[cur].explicit, prev) && !strings.EqualFold(res[cur].task.ID, prev) {
				res[cur].explicit = append(res[cur].explicit, prev)
			}
		}
	}
	// Page-level sequential: chain all leaf Tasks on the page.
	pagesSeen := map[string]bool{}
	for _, x := range res {
		key := norm.ID(x.page.Name)
		if pagesSeen[key] {
			continue
		}
		pagesSeen[key] = true
		if text := r.pageDefault(x.page.Name, "order", ""); text != "" {
			if m, err := parse.ParseOrder(text); err == nil && m == model.OrderSequential {
				var leaves []int
				for idx, y := range res {
					if isContainer[idx] {
						continue
					}
					if norm.ID(y.page.Name) == key {
						leaves = append(leaves, idx)
					}
				}
				slices.SortFunc(leaves, func(a, b int) int { return res[a].task.OutlineOrder - res[b].task.OutlineOrder })
				for k := 1; k < len(leaves); k++ {
					prev := res[leaves[k-1]].task.ID
					cur := leaves[k]
					if !slices.Contains(res[cur].explicit, prev) {
						res[cur].explicit = append(res[cur].explicit, prev)
					}
				}
			}
		}
	}

	// Finalize Depends: sorted deduped (keep unknown/sentinel for blocking).
	for i := range res {
		seen := map[string]bool{}
		var out []string
		for _, d := range res[i].explicit {
			key := norm.ID(d)
			// Preserve original case for Task IDs? Use stored ID case.
			display := d
			if j, ok := byID[key]; ok {
				display = res[j].task.ID
				key = strings.ToLower(display)
			}
			if !seen[key] {
				seen[key] = true
				out = append(out, display)
			}
		}
		slices.Sort(out)
		res[i].task.Depends = out
	}

	// Cycle detection over Task-to-Task edges (ignore unknown/sentinel).
	adj := make(map[int][]int)
	for i := range res {
		for _, dep := range res[i].task.Depends {
			if j, ok := byID[norm.ID(dep)]; ok {
				adj[i] = append(adj[i], j)
			}
		}
	}
	inCycle := findCycleNodes(len(res), adj)
	for i := range res {
		if inCycle[i] {
			r.issue(IssueDependencyCycle, taskContext(res[i].task),
				"depends:: / sequential-chain cycle involves this Task; Tasks in the cycle are left unscheduled")
		}
	}

	for _, x := range res {
		r.tasks = append(r.tasks, x.task)
	}

	for _, t := range r.tasks {
		for _, slug := range t.TimeMaps {
			if !r.mapSlugs[slug] {
				r.issue(IssueUnknownTimeMap, taskContext(t),
					fmt.Sprintf("time-map:: %s references no Time Map defined on %q", slug, r.in.TimeMapsPage))
			}
		}
		// order:: validation already reported in task(); unknown order
		// values fall back to parallel with a bad-property issue.
	}
}

// findCycleNodes returns the nodes on a dependency cycle via DFS colouring:
// white unvisited, grey on the current stack, black done. When an edge hits
// a grey node, every node from that target up to the stack top is on a cycle.
func findCycleNodes(n int, adj map[int][]int) map[int]bool {
	const (
		white = 0
		grey  = 1
		black = 2
	)
	colour := make([]int, n)
	inCycle := map[int]bool{}
	var stack []int
	var visit func(int)
	visit = func(u int) {
		colour[u] = grey
		stack = append(stack, u)
		for _, v := range adj[u] {
			if colour[v] == white {
				visit(v)
			} else if colour[v] == grey {
				for k := len(stack) - 1; k >= 0; k-- {
					inCycle[stack[k]] = true
					if stack[k] == v {
						break
					}
				}
			}
		}
		stack = stack[:len(stack)-1]
		colour[u] = black
	}
	for i := 0; i < n; i++ {
		if colour[i] == white {
			visit(i)
		}
	}
	return inCycle
}

// taskWithAncestors resolves one candidate block with ancestor-aware
// inheritance: time-map, priority, plan-start/end and order cascade
// block > ancestors (nearest first) > page; duration, deadline, scheduled
// and depends never inherit.
func (r *resolver) taskWithAncestors(page *logseq.Page, b *logseq.Block, ancestors []*logseq.Block) (model.Task, bool, *Issue) {
	t := model.Task{
		ID:         strings.TrimSpace(b.UUID),
		Graph:      r.in.Graph,
		GraphOrder: r.in.GraphOrder,
		Text:       taskText(b),
		Marker:     model.Marker(strings.ToUpper(strings.TrimSpace(b.Marker))),
	}

	if t.ID == "" {
		return t, false, &Issue{
			Kind:    IssueMissingBlockID,
			Context: r.pageContext(page.Name),
			Detail:  "the task-marked block has no UUID, so it cannot be tracked across runs",
		}
	}

	rawDur, ok := blockProps(b)["duration"]
	if !ok {
		return t, false, &Issue{
			Kind:    IssueMissingDuration,
			Context: r.blockContext(page, b),
			Detail:  fmt.Sprintf("%q has no duration::, so it is not scheduled; a TODO becomes a Task only with an explicit duration (ADR 0004)", t.Text),
		}
	}
	durText, ok := propsText(rawDur)
	if !ok {
		return t, false, &Issue{
			Kind:    IssueBadProperty,
			Context: r.blockContext(page, b),
			Detail:  fmt.Sprintf("duration:: has a malformed value the app parsed as %v; the block is not scheduled", rawDur),
		}
	}
	dur, durErr := parse.ParseDuration(durText)
	if durErr != nil {
		return t, false, &Issue{
			Kind:    IssueBadProperty,
			Context: r.blockContext(page, b),
			Detail:  fmt.Sprintf("duration:: %q is unparseable (%v); the block is not scheduled", durText, durErr),
		}
	}
	t.Duration = dur

	if text := r.inheritedValue(page, b, ancestors, "time-map"); text != "" {
		for _, slug := range splitSlugs(text) {
			if !slices.Contains(t.TimeMaps, slug) {
				t.TimeMaps = append(t.TimeMaps, slug)
			}
		}
	} else if r.in.DefaultTimeMap != "" {
		// Graph-level fallback: explicit time-map:: on block, parent or
		// page wins; otherwise the graph default applies before the
		// global default:: true map.
		if slug := parse.Slugify(r.in.DefaultTimeMap); slug != "" {
			t.TimeMaps = []string{slug}
		}
	}

	if text := r.inheritedValue(page, b, ancestors, "priority"); text != "" {
		p, pErr := parse.ParsePriority(text)
		if pErr != nil {
			r.appendBad(r.blockContext(page, b), "priority", text, pErr)
		} else {
			t.Priority = p
		}
	}

	if text := r.blockValue(page, b, "deadline"); text != "" {
		d, dErr := parseDate(text)
		if dErr != nil {
			r.appendBad(r.blockContext(page, b), "deadline", text, dErr)
		} else {
			t.Deadline = &d
		}
	}

	if text := r.inheritedValue(page, b, ancestors, "plan-start"); text != "" {
		d, pErr := parse.ParseCivilDate(text)
		if pErr != nil {
			r.appendBad(r.blockContext(page, b), "plan-start", text, pErr)
		} else {
			t.PlanStart = &d
		}
	}
	if text := r.inheritedValue(page, b, ancestors, "plan-end"); text != "" {
		d, pErr := parse.ParseCivilDate(text)
		if pErr != nil {
			r.appendBad(r.blockContext(page, b), "plan-end", text, pErr)
		} else {
			t.PlanEnd = &d
		}
	}

	if text := r.inheritedValue(page, b, ancestors, "order"); text != "" {
		m, oErr := parse.ParseOrder(text)
		if oErr != nil {
			r.appendBad(r.blockContext(page, b), "order", text, oErr)
		} else if m == model.OrderSequential {
			t.Order = m
		}
	}

	if text := r.blockValue(page, b, "scheduled"); text != "" {
		stamp, sErr := parse.ParseOrgStamp(text)
		switch {
		case sErr != nil:
			r.appendBad(r.blockContext(page, b), "scheduled", text, sErr)
		case !stamp.HasTime:
			r.appendBad(r.blockContext(page, b), "scheduled", text,
				fmt.Errorf("no time: a Pin is an exact date-time and the day-granular equivalent is plan-start or plan-end"))
		default:
			pin := stamp.Instant(r.in.Location)
			t.Pin = &pin
		}
	}

	// depends:: — block-level only, strict comma ((uuid)) list.
	if text := r.blockValue(page, b, "depends"); text != "" {
		deps, dErr := parse.ParseDepends(text)
		if dErr != nil {
			r.appendBad(r.blockContext(page, b), "depends", text, dErr)
			t.Depends = []string{fmt.Sprintf("malformed-depends:%s", norm.ID(t.ID))}
		} else {
			t.Depends = deps
		}
	}

	var unknown []string
	for key, v := range blockProps(b) {
		if !knownKeys[key] {
			unknown = append(unknown, key)
			continue
		}
		_ = v
	}
	slices.Sort(unknown)
	for _, key := range unknown {
		detail := fmt.Sprintf("%q is not a property lodo reads (it reads duration::, time-map::, priority::, plan-start::, plan-end::, deadline::, scheduled::, depends::, order::)", key)
		if text, ok := propsText(blockProps(b)[key]); ok && text != "" {
			detail += fmt.Sprintf(" Value %q is left alone.", text)
		}
		r.issue(IssueUnknownProperty, r.blockContext(page, b), detail)
	}

	return t, true, nil
}

// inheritedValue resolves an inheritable property: block explicit, then
// ancestors nearest-first, then the page default. A blank explicit value
// falls through; a malformed block value issues and stops (ancestors are
// skipped, matching historic behavior).
func (r *resolver) inheritedValue(page *logseq.Page, b *logseq.Block, ancestors []*logseq.Block, key string) string {
	if raw, ok := blockProps(b)[key]; ok {
		text, okT := propsText(raw)
		if !okT {
			r.issue(IssueBadProperty, r.blockContext(page, b),
				fmt.Sprintf("%s:: has a malformed value the app parsed as %v; the property is skipped", key, raw))
			return ""
		}
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			return trimmed
		}
	}
	for k := len(ancestors) - 1; k >= 0; k-- {
		if v := blockTextOnly(ancestors[k], key); v != "" {
			return v
		}
	}
	if !inheritableKeys[key] || page == nil {
		return ""
	}
	return r.pageDefault(page.Name, key, "")
}

// blockValue is the block-level value only — for keys that never inherit.
func (r *resolver) blockValue(page *logseq.Page, b *logseq.Block, key string) string {
	return r.blockText(page, b, key, true)
}

// blockText reads one block's property text, reporting malformed values.
func (r *resolver) blockText(page *logseq.Page, b *logseq.Block, key string, report bool) string {
	raw, ok := blockProps(b)[key]
	if !ok {
		return ""
	}
	text, okT := propsText(raw)
	if !okT {
		if report {
			r.issue(IssueBadProperty, r.blockContext(page, b),
				fmt.Sprintf("%s:: has a malformed value the app parsed as %v; the property is skipped", key, raw))
		}
		return ""
	}
	return strings.TrimSpace(text)
}

func blockTextOnly(b *logseq.Block, key string) string {
	raw, ok := blockProps(b)[key]
	if !ok {
		return ""
	}
	text, okT := propsText(raw)
	if !okT {
		return ""
	}
	return strings.TrimSpace(text)
}

// pageDefault reads or sets the inheritable page default for key on page.
func (r *resolver) pageDefault(pageName, key, text string) string {
	at := fmt.Sprintf("%s\x00%s", norm.ID(pageName), key)
	if text == "" {
		return r.pageProps[at]
	}
	r.pageProps[at] = strings.TrimSpace(text)
	return text
}

// splitSlugs parses a time-map:: value "deep-work, weekends" into its
// case-normalised slugs; empty and blank pieces vanish.
func splitSlugs(text string) []string {
	var out []string
	for _, piece := range strings.Split(text, ",") {
		if slug := parse.Slugify(piece); slug != "" {
			out = append(out, slug)
		}
	}
	return out
}

// parseDate accepts the native org stamp and the bare date for deadline::.
func parseDate(s string) (model.CivilDate, error) {
	if stamp, err := parse.ParseOrgStamp(s); err == nil {
		return stamp.Date, nil
	}
	return parse.ParseCivilDate(s)
}

// taskText is the block's display text: its first non-property line with
// the marker prefix removed.
func taskText(b *logseq.Block) string {
	text := stripProps(b.Content)
	if text == "" {
		return ""
	}
	first, _, _ := strings.Cut(text, "\n")
	first = strings.TrimSpace(first)
	if marker := strings.TrimSpace(b.Marker); marker != "" && len(first) > len(marker) &&
		strings.EqualFold(first[:len(marker)], marker) {
		first = strings.TrimSpace(first[len(marker):])
	}
	return first
}

// appendBad adds a bad-property-value issue with the value quoted.
func (r *resolver) appendBad(context, key, raw string, err error) {
	r.issue(IssueBadProperty, context, fmt.Sprintf("%s:: %q: %v", key, raw, err))
}

// parseExplicitOrder parses a block's own order:: value without inheritance.
// ok is false when the block carries no usable value (absent, blank or
// unparseable), in which case callers fall through to ancestors/page.
func parseExplicitOrder(b *logseq.Block) (model.OrderMode, bool) {
	raw, ok := blockProps(b)["order"]
	if !ok {
		return "", false
	}
	text, okT := propsText(raw)
	if !okT || strings.TrimSpace(text) == "" {
		return "", false
	}
	m, err := parse.ParseOrder(text)
	if err != nil {
		return "", false
	}
	return m, true
}
