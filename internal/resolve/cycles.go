package resolve

import (
	"slices"

	"github.com/aries1980/lodo/internal/norm"
	"github.com/aries1980/lodo/internal/set"
)

// detectCycles reports dependency/chain cycles over Task-to-Task edges,
// ignoring unknown/sentinel entries.
func (r *resolver) detectCycles(graph *taskGraph) {
	res := graph.resolved
	adj := make(map[int][]int)
	for i := range res {
		for _, dep := range res[i].task.Depends {
			if j, ok := graph.byID[norm.ID(dep)]; ok {
				adj[i] = append(adj[i], j)
			}
		}
	}
	inCycle := findCycleNodes(len(res), adj)
	for i := range res {
		if inCycle.Has(i) {
			r.issue(IssueDependencyCycle, taskContext(res[i].task),
				"depends:: / sequential-chain cycle involves this Task; Tasks in the cycle are left unscheduled")
		}
	}
}

// findCycleNodes returns the nodes on a dependency cycle via DFS colouring:
// white unvisited, grey on the current stack, black done. When an edge hits
// a grey node, every node from that target up to the stack top is on a cycle.
func findCycleNodes(n int, adj map[int][]int) set.Set[int] {
	const (
		white = 0
		grey  = 1
		black = 2
	)
	colour := make([]int, n)
	inCycle := set.New[int]()
	var stack []int
	var visit func(int)
	visit = func(u int) {
		colour[u] = grey
		stack = append(stack, u)
		for _, v := range adj[u] {
			if colour[v] == white {
				visit(v)
			} else if colour[v] == grey {
				for _, node := range slices.Backward(stack) {
					inCycle.Add(node)
					if node == v {
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
