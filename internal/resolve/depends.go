package resolve

import (
	"fmt"
	"slices"
	"strings"

	"github.com/aries1980/lodo/internal/norm"
)

// validateExplicitDepends expands container refs to leaf Tasks, drops
// DONE/CANCELLED satisfies, and keeps unknown refs (reported) so the
// planner can block and report them.
func (r *resolver) validateExplicitDepends(graph *taskGraph, doneSet map[string]bool) {
	res := graph.resolved
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
			if j, ok := graph.byID[lower]; ok {
				if graph.isContainer[j] {
					for _, leaf := range graph.leavesOf(j) {
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
}

// finalizeDepends sorts and dedupes effective Depends, keeping
// unknown/sentinel entries for blocking. Task IDs use stored case.
func finalizeDepends(graph *taskGraph) {
	res := graph.resolved
	for i := range res {
		seen := map[string]bool{}
		var out []string
		for _, dep := range res[i].explicit {
			key := norm.ID(dep)
			display := dep
			if j, ok := graph.byID[key]; ok {
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
}
