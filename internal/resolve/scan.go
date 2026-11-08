package resolve

import (
	"fmt"
	"strings"

	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/norm"
)

// candidate is one TODO/DOING block that may become a Task, with its
// root-first ancestor stack for inheritance and order chaining.
type candidate struct {
	page      *logseq.Page
	block     *logseq.Block
	ancestors []*logseq.Block
}

// everyBlock is every block walked, for sequential order chaining.
type everyBlock struct {
	page      *logseq.Page
	block     *logseq.Block
	ancestors []*logseq.Block
}

// blockCollection is the raw walk output: candidates plus every block and
// the DONE/CANCELLED UUID set (lower-cased) for depends validation.
type blockCollection struct {
	candidates []candidate
	all        []everyBlock
	doneSet    map[string]bool
}

// collectPageDefaults records inheritable page defaults before the walk.
func (r *resolver) collectPageDefaults() {
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
}

// collectCandidates walks every page but the Time Maps page depth-first,
// returning candidates in outline order (pages sorted, depth-first).
func (r *resolver) collectCandidates() blockCollection {
	var cands []candidate
	doneSet := map[string]bool{}
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
			for _, block := range blocks {
				if block == nil {
					continue
				}
				all = append(all, everyBlock{page: page, block: block, ancestors: append([]*logseq.Block(nil), ancestors...)})
				marker := model.Marker(strings.ToUpper(strings.TrimSpace(block.Marker)))
				if marker == model.MarkerDONE || marker == model.MarkerCANCELLED {
					if id := norm.ID(block.UUID); id != "" {
						doneSet[id] = true
					}
				}
				// Suppression: any DONE/CANCELLED ancestor hides the subtree.
				suppressed := false
				for _, ancestor := range ancestors {
					ancestorMarker := model.Marker(strings.ToUpper(strings.TrimSpace(ancestor.Marker)))
					if ancestorMarker == model.MarkerDONE || ancestorMarker == model.MarkerCANCELLED {
						suppressed = true
						break
					}
				}
				switch marker {
				case model.MarkerTODO, model.MarkerDOING:
					if !suppressed {
						cands = append(cands, candidate{page: page, block: block, ancestors: append([]*logseq.Block(nil), ancestors...)})
					}
				}
				walk(block.Children, append(ancestors, block))
			}
		}
		walk(tree, nil)
	}
	return blockCollection{candidates: cands, all: all, doneSet: doneSet}
}
