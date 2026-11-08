package resolve_test

import (
	"testing"

	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/resolve"
)

func TestProvenanceRecordsInheritance(t *testing.T) {
	pages := []*logseq.Page{
		page("Work", map[string]any{"time-map": "deep", "priority": "A"}),
		page("Time Maps", nil),
	}
	trees := map[string][]*logseq.Block{
		"Time Maps": {
			props(block("m1", "Deep"), map[string]any{"default": true}),
			{UUID: "w1", Content: "Mon-Fri 09:00-12:00"},
		},
		"Work": {
			props(marked("TODO", "t1", "TODO Inherited task"), map[string]any{"duration": "30m"}),
			props(marked("TODO", "t2", "TODO Direct task"), map[string]any{"duration": "45m", "priority": "C", "time-map": "deep"}),
		},
	}
	// Fix Time Maps structure: windows are children of map blocks.
	trees["Time Maps"] = []*logseq.Block{
		{
			UUID: "m1", Content: "Deep",
			Properties: map[string]any{"default": true},
			Children:   []*logseq.Block{{UUID: "w1", Content: "Mon-Fri 09:00-12:00"}},
		},
	}
	res, err := resolve.Resolve(resolve.Input{
		Pages: pages, Trees: trees, TimeMapsPage: "Time Maps", Location: london,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	p1, ok := res.Provenance["t1"]
	if !ok {
		t.Fatalf("missing provenance for t1: %+v", res.Provenance)
	}
	hasPagePriority := false
	hasDirectDuration := false
	for _, p := range p1.Properties {
		if p.Property == "priority" && p.Source == "page" && p.Inherited {
			hasPagePriority = true
		}
		if p.Property == "duration" && p.Source == "direct" && !p.Inherited {
			hasDirectDuration = true
		}
	}
	if !hasPagePriority {
		t.Fatalf("t1 should inherit priority from page: %+v", p1.Properties)
	}
	if !hasDirectDuration {
		t.Fatalf("t1 duration should be authored on the block: %+v", p1.Properties)
	}
	p2, ok := res.Provenance["t2"]
	if !ok {
		t.Fatalf("missing provenance for t2")
	}
	for _, p := range p2.Properties {
		if p.Property == "priority" && p.Source == "direct" {
			return
		}
	}
	t.Fatalf("t2 priority should be direct: %+v", p2.Properties)
}
