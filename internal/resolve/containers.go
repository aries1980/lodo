package resolve

import (
	"slices"

	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/norm"
)

// resolvedTask is one resolved candidate plus its explicit depends before
// container expansion and sequential chaining.
type resolvedTask struct {
	task      model.Task
	page      *logseq.Page
	block     *logseq.Block
	ancestors []*logseq.Block
	explicit  []string
}

// taskGraph indexes resolved tasks for hierarchy and dependency passes.
type taskGraph struct {
	resolved    []resolvedTask
	byID        map[string]int
	descendants map[int][]int
	isContainer map[int]bool
}

// resolveCandidates turns candidates into resolved tasks in outline order.
// OutlineOrder counts TODO/DOING candidates as before.
func (r *resolver) resolveCandidates(cands []candidate) []resolvedTask {
	order := 0
	var res []resolvedTask
	for _, cand := range cands {
		task, isTask, issue := r.taskWithAncestors(cand.page, cand.block, cand.ancestors)
		if issue != nil {
			r.issue(issue.Kind, issue.Context, issue.Detail)
		}
		if isTask {
			task.OutlineOrder = order
			res = append(res, resolvedTask{task: task, page: cand.page, block: cand.block, ancestors: cand.ancestors, explicit: append([]string(nil), task.Depends...)})
		}
		order++
	}
	return res
}

// indexTaskGraph links descendants, marks containers and sets ParentID.
// For each task, ancestors are walked once: O(n*depth) not O(n²).
func indexTaskGraph(res []resolvedTask) taskGraph {
	byID := map[string]int{}
	for i, resolved := range res {
		byID[norm.ID(resolved.task.ID)] = i
	}
	descendants := make(map[int][]int)
	for i, resolved := range res {
		seen := map[int]bool{}
		for _, ancestor := range resolved.ancestors {
			id := norm.ID(ancestor.UUID)
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
	for j, leaves := range descendants {
		if len(leaves) > 0 {
			isContainer[j] = true
		}
	}
	// ParentID: nearest ancestor that is a Task.
	for i, resolved := range res {
		for k := len(resolved.ancestors) - 1; k >= 0; k-- {
			id := norm.ID(resolved.ancestors[k].UUID)
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
	return taskGraph{resolved: res, byID: byID, descendants: descendants, isContainer: isContainer}
}

// leavesOf returns a container's transitive leaf task indices in outline
// order; a non-container returns itself.
func (g *taskGraph) leavesOf(j int) []int {
	if !g.isContainer[j] {
		return []int{j}
	}
	var out []int
	for _, descendant := range g.descendants[j] {
		if !g.isContainer[descendant] {
			out = append(out, descendant)
		}
	}
	slices.SortFunc(out, func(a, b int) int { return g.resolved[a].task.OutlineOrder - g.resolved[b].task.OutlineOrder })
	return out
}
