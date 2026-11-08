package resolve

import (
	"slices"
	"strings"

	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/norm"
	"github.com/aries1980/lodo/internal/parse"
)

// orderCache parses explicit order:: values once per block.
type orderCache struct {
	modes map[*logseq.Block]model.OrderMode
	has   map[*logseq.Block]bool
}

func newOrderCache() *orderCache {
	return &orderCache{modes: make(map[*logseq.Block]model.OrderMode), has: make(map[*logseq.Block]bool)}
}

func (c *orderCache) explicitOrder(block *logseq.Block) (model.OrderMode, bool) {
	if block == nil {
		return "", false
	}
	if v, ok := c.has[block]; ok {
		return c.modes[block], v
	}
	mode, ok := parseExplicitOrder(block)
	c.modes[block] = mode
	c.has[block] = ok
	return mode, ok
}

// effectiveOrder resolves block explicit > ancestors > page > parallel.
func (r *resolver) effectiveOrder(cache *orderCache, page *logseq.Page, ancestors []*logseq.Block, block *logseq.Block) model.OrderMode {
	if mode, ok := cache.explicitOrder(block); ok {
		return mode
	}
	for k := len(ancestors) - 1; k >= 0; k-- {
		if mode, ok := cache.explicitOrder(ancestors[k]); ok {
			return mode
		}
	}
	if page != nil {
		if text := r.pageDefault(page.Name, "order", ""); text != "" {
			if mode, err := parse.ParseOrder(text); err == nil {
				return mode
			}
		}
	}
	return model.OrderParallel
}

// chainSequential adds implicit edges for every sequential block and page:
// descendant leaf Tasks chain in outline order.
func (r *resolver) chainSequential(graph *taskGraph, all []everyBlock) {
	res := graph.resolved
	cache := newOrderCache()
	// Ancestor block ID -> descendant leaf task indices (outline order).
	leavesByAncestor := make(map[string][]int)
	for idx := range res {
		if graph.isContainer[idx] {
			continue
		}
		for _, ancestor := range res[idx].ancestors {
			id := norm.ID(ancestor.UUID)
			if id == "" {
				continue
			}
			leavesByAncestor[id] = append(leavesByAncestor[id], idx)
		}
	}
	for _, entry := range all {
		if r.effectiveOrder(cache, entry.page, entry.ancestors, entry.block) != model.OrderSequential {
			continue
		}
		id := norm.ID(entry.block.UUID)
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
	r.chainPageSequential(graph)
}

// chainPageSequential chains all leaf Tasks on a page with page-level
// order:: sequential.
func (r *resolver) chainPageSequential(graph *taskGraph) {
	res := graph.resolved
	pagesSeen := map[string]bool{}
	for _, resolved := range res {
		key := norm.ID(resolved.page.Name)
		if pagesSeen[key] {
			continue
		}
		pagesSeen[key] = true
		if text := r.pageDefault(resolved.page.Name, "order", ""); text != "" {
			if mode, err := parse.ParseOrder(text); err == nil && mode == model.OrderSequential {
				var leaves []int
				for idx, other := range res {
					if graph.isContainer[idx] {
						continue
					}
					if norm.ID(other.page.Name) == key {
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
}

// parseExplicitOrder parses a block's own order:: value without inheritance.
// ok is false when the block carries no usable value (absent, blank or
// unparseable), in which case callers fall through to ancestors/page.
func parseExplicitOrder(block *logseq.Block) (model.OrderMode, bool) {
	raw, ok := blockProps(block)["order"]
	if !ok {
		return "", false
	}
	text, okT := propsText(raw)
	if !okT || strings.TrimSpace(text) == "" {
		return "", false
	}
	mode, err := parse.ParseOrder(text)
	if err != nil {
		return "", false
	}
	return mode, true
}
