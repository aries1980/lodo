package resolve_test

import (
	"testing"

	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/resolve"
)

func TestExplicitDependsResolved(t *testing.T) {
	res, err := fixture([]*logseq.Page{page(day)}, map[string][]*logseq.Block{day: {
		props(marked("TODO", id1, "TODO A"), map[string]any{"duration": "1h"}),
		props(marked("TODO", id2, "TODO B"), map[string]any{"duration": "1h", "depends": "((" + id1 + "))"}),
	}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Tasks) != 2 {
		t.Fatalf("Tasks=%v want 2", taskIDs(res))
	}
	var bDepends []string
	for _, task := range res.Tasks {
		if task.ID == id2 {
			bDepends = task.Depends
		}
	}
	if len(bDepends) != 1 || bDepends[0] != id1 {
		t.Fatalf("B Depends=%v want [%s]", bDepends, id1)
	}
	if len(res.Issues) != 0 {
		t.Fatalf("Issues=%+v want none", res.Issues)
	}
}

func TestDoneSatisfiesDepends(t *testing.T) {
	doneID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	res, err := fixture([]*logseq.Page{page(day)}, map[string][]*logseq.Block{day: {
		props(marked("DONE", doneID, "DONE Old"), map[string]any{"duration": "1h"}),
		props(marked("TODO", id1, "TODO New"), map[string]any{"duration": "1h", "depends": "((" + doneID + "))"}),
	}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Tasks) != 1 || len(res.Tasks[0].Depends) != 0 {
		t.Fatalf("DONE dep must satisfy: Tasks=%+v", res.Tasks)
	}
}

func TestUnknownDependsIsReported(t *testing.T) {
	ghost := "99999999-9999-4999-8999-999999999999"
	res, err := fixture([]*logseq.Page{page(day)}, map[string][]*logseq.Block{day: {
		props(marked("TODO", id1, "TODO Lonely"), map[string]any{"duration": "1h", "depends": "((" + ghost + "))"}),
	}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	found := false
	for _, is := range res.Issues {
		if is.Kind == resolve.IssueUnknownDependency {
			found = true
		}
	}
	if !found {
		t.Fatalf("want unknown-dependency issue, got %+v", res.Issues)
	}
	if len(res.Tasks[0].Depends) != 1 {
		t.Fatalf("dangling edge must be kept for planner blocking: %+v", res.Tasks[0])
	}
}

func TestMalformedDependsIsReported(t *testing.T) {
	res, err := fixture([]*logseq.Page{page(day)}, map[string][]*logseq.Block{day: {
		props(marked("TODO", id1, "TODO Bad"), map[string]any{"duration": "1h", "depends": "hello"}),
	}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	found := false
	for _, is := range res.Issues {
		if is.Kind == resolve.IssueBadProperty {
			found = true
		}
	}
	if !found {
		t.Fatalf("want bad-property issue, got %+v", res.Issues)
	}
}

func TestSelfDependsIsCycle(t *testing.T) {
	res, err := fixture([]*logseq.Page{page(day)}, map[string][]*logseq.Block{day: {
		props(marked("TODO", id1, "TODO Self"), map[string]any{"duration": "1h", "depends": "((" + id1 + "))"}),
	}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	found := false
	for _, is := range res.Issues {
		if is.Kind == resolve.IssueDependencyCycle {
			found = true
		}
	}
	if !found {
		t.Fatalf("want dependency-cycle issue, got %+v", res.Issues)
	}
}

func TestContainerParentIsMarked(t *testing.T) {
	res, err := fixture([]*logseq.Page{page(day)}, map[string][]*logseq.Block{day: {
		props(marked("TODO", id1, "TODO Project", marked("TODO", id2, "TODO Child")), map[string]any{"duration": "2h"}),
	}})
	_ = res
	_ = err
	// Note: fixture helper builds Children via block() args; the above nests
	// via Content, not Children. Use explicit Children nesting:
	res2, err := fixture([]*logseq.Page{page(day)}, map[string][]*logseq.Block{day: {
		func() *logseq.Block {
			child := props(marked("TODO", id2, "TODO Child"), map[string]any{"duration": "1h"})
			parent := props(marked("TODO", id1, "TODO Project"), map[string]any{"duration": "2h"})
			parent.Children = []*logseq.Block{child}
			return parent
		}(),
	}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	var parent, child *struct{ Container bool }
	_ = parent
	_ = child
	foundParent, foundChild := false, false
	for _, task := range res2.Tasks {
		if task.ID == id1 {
			foundParent = true
			if !task.Container {
				t.Fatalf("parent with Task child must be container: %+v", task)
			}
		}
		if task.ID == id2 {
			foundChild = true
			if task.Container {
				t.Fatalf("leaf must not be container: %+v", task)
			}
			if task.ParentID != id1 {
				t.Fatalf("child ParentID=%q want %s", task.ParentID, id1)
			}
		}
	}
	if !foundParent || !foundChild {
		t.Fatalf("Tasks=%v want both", taskIDs(res2))
	}
}

func TestSequentialChainsLeaves(t *testing.T) {
	mkchild := func(id, text string) *logseq.Block {
		return props(marked("TODO", id, "TODO "+text), map[string]any{"duration": "30m"})
	}
	parent := props(marked("TODO", id1, "TODO Seq"), map[string]any{"duration": "1h", "order": "sequential"})
	parent.Children = []*logseq.Block{mkchild(id2, "First"), mkchild(id3, "Second")}
	res, err := fixture([]*logseq.Page{page(day)}, map[string][]*logseq.Block{day: {parent}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for _, task := range res.Tasks {
		if task.ID == id3 {
			if len(task.Depends) != 1 || task.Depends[0] != id2 {
				t.Fatalf("second must depend on first via sequential: %+v", task)
			}
		}
		if task.ID == id2 && len(task.Depends) != 0 {
			t.Fatalf("first must have no deps: %+v", task)
		}
	}
}

func TestDoneParentSuppressesSubtree(t *testing.T) {
	child := props(marked("TODO", id2, "TODO Child"), map[string]any{"duration": "1h"})
	parent := marked("DONE", id1, "DONE Project")
	parent.Children = []*logseq.Block{child}
	res, err := fixture([]*logseq.Page{page(day)}, map[string][]*logseq.Block{day: {parent}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Tasks) != 0 {
		t.Fatalf("DONE parent must suppress subtree: Tasks=%v", taskIDs(res))
	}
}

func TestBlockCascadePlan(t *testing.T) {
	child := props(marked("TODO", id2, "TODO Child"), map[string]any{"duration": "1h"})
	parent := props(marked("TODO", id1, "TODO Project"), map[string]any{"duration": "2h", "plan-end": "2026-10-24"})
	parent.Children = []*logseq.Block{child}
	res, err := fixture([]*logseq.Page{page(day)}, map[string][]*logseq.Block{day: {parent}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for _, task := range res.Tasks {
		if task.ID == id2 && (task.PlanEnd == nil || task.PlanEnd.Day != 24) {
			t.Fatalf("child must inherit parent plan-end: %+v", task)
		}
	}
}
