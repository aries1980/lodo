package run_test

import (
	"context"
	"testing"
	"time"

	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/run"
)

func graphSource(name string, order int, g graphFixture, defaultMap string) run.Source {
	return run.Source{
		Name:           name,
		Order:          order,
		Client:         newStubClient(g),
		DefaultTimeMap: defaultMap,
	}
}

func TestReadSourcesMerges(t *testing.T) {
	primary := graphFixture{
		pages: []*logseq.Page{memPage("a"), memPage("Time Maps")},
		trees: map[string][]*logseq.Block{
			"Time Maps": {mapDeepWork},
			"a": {
				props(marked("TODO", "t-work", "TODO Work task"), map[string]any{"duration": "30m"}),
			},
		},
	}
	secondary := graphFixture{
		pages: []*logseq.Page{memPage("b"), memPage("Time Maps")},
		trees: map[string][]*logseq.Block{
			// Secondary Time Maps page must be ignored (canonical is primary's).
			"Time Maps": {
				props(block("m-x", "Other"), map[string]any{"default": true}),
			},
			"b": {
				props(marked("TODO", "t-fam", "TODO Family task"), map[string]any{"duration": "30m"}),
			},
		},
	}
	sources := []run.Source{
		graphSource("work", 0, primary, "deep-work"),
		graphSource("family", 1, secondary, ""),
	}
	res, err := run.ReadSources(context.Background(), sources, "Time Maps", london)
	if err != nil {
		t.Fatalf("ReadSources: %v", err)
	}
	if len(res.Tasks) != 2 {
		t.Fatalf("Tasks = %+v, want 2 merged", res.Tasks)
	}
	byID := map[string]string{}
	for _, task := range res.Tasks {
		byID[task.ID] = task.Graph
	}
	if byID["t-work"] != "work" || byID["t-fam"] != "family" {
		t.Fatalf("graphs = %+v, want work/family", byID)
	}
	// Time Maps come from primary only.
	if len(res.TimeMaps) != 1 || res.TimeMaps[0].Slug != "deep-work" {
		t.Fatalf("TimeMaps = %+v, want primary deep-work", res.TimeMaps)
	}
}

func TestGraphSourcesPlansAcrossGraphs(t *testing.T) {
	primary := graphFixture{
		pages: []*logseq.Page{memPage("a"), memPage("Time Maps")},
		trees: map[string][]*logseq.Block{
			"Time Maps": {mapDeepWork},
			"a": {
				props(marked("TODO", "t-work", "TODO Work task"), map[string]any{"duration": "30m"}),
			},
		},
	}
	secondary := graphFixture{
		pages: []*logseq.Page{memPage("b")},
		trees: map[string][]*logseq.Block{
			"b": {
				props(marked("TODO", "t-fam", "TODO Family task"), map[string]any{"duration": "30m"}),
			},
		},
	}
	cfg := testConfig()
	now := at(9, 8, 0)
	outcome, err := run.GraphSources(context.Background(), []run.Source{
		graphSource("work", 0, primary, "deep-work"),
		graphSource("family", 1, secondary, "deep-work"),
	}, nil, cfg, now)
	if err != nil {
		t.Fatalf("GraphSources: %v", err)
	}
	if len(outcome.Schedule.Blocks) != 2 {
		t.Fatalf("Blocks = %+v, want both graphs placed", outcome.Schedule.Blocks)
	}
	// Config order decides ties: work (priority equal, same hours) first.
	if outcome.Schedule.Blocks[0].TaskID != "t-work" {
		t.Fatalf("first block = %s, want t-work (graph order)", outcome.Schedule.Blocks[0].TaskID)
	}
	_ = time.Now
	_ = config.Config{}
}
