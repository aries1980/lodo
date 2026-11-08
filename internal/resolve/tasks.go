package resolve

import (
	"fmt"
)

// resolveTasks builds Tasks from every page but the Time Maps page: a block
// is a Task iff it is TODO/DOING and carries an explicit duration::
// (ADR 0004); anything else is a note, skipped — and reported when the
// omission would otherwise be silent.
//
// Nesting and dependencies (ADR 0011, SkedPal parity): a Task with Task
// descendants is a container, never scheduled itself; DONE/CANCELLED ancestors
// suppress their subtree; order:: sequential chains leaf Tasks in outline order;
// depends:: adds explicit finish-to-start edges.
//
// Flow: collectPageDefaults → collectCandidates → resolveCandidates →
// indexTaskGraph → validateExplicitDepends → chainSequential →
// finalizeDepends → detectCycles → emitTasks. Each phase lives in its own
// file so the pipeline reads top-down.
func (r *resolver) resolveTasks() {
	r.collectPageDefaults()
	collected := r.collectCandidates()
	resolved := r.resolveCandidates(collected.candidates)
	graph := indexTaskGraph(resolved)
	r.validateExplicitDepends(&graph, collected.doneSet)
	r.chainSequential(&graph, collected.all)
	finalizeDepends(&graph)
	r.detectCycles(&graph)
	r.emitTasks(&graph)
}

// emitTasks appends resolved tasks and reports unknown Time Maps.
func (r *resolver) emitTasks(graph *taskGraph) {
	for _, resolved := range graph.resolved {
		r.tasks = append(r.tasks, resolved.task)
	}
	for _, task := range r.tasks {
		for _, slug := range task.TimeMaps {
			if !r.mapSlugs[slug] {
				r.issue(IssueUnknownTimeMap, taskContext(task),
					fmt.Sprintf("time-map:: %s references no Time Map defined on %q", slug, r.in.TimeMapsPage))
			}
		}
		// order:: validation already reported in task(); unknown order
		// values fall back to parallel with a bad-property issue.
	}
}
