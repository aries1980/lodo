package resolve_test

import (
	"testing"

	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/resolve"
)

func TestGraphDefaultApplies(t *testing.T) {
	res, err := resolve.Resolve(resolve.Input{
		Graph:          "work",
		GraphOrder:     0,
		DefaultTimeMap: "deep-work",
		Pages:          []*logseq.Page{page(day)},
		Trees: map[string][]*logseq.Block{
			day: {props(marked("TODO", id1, "TODO Do the work"), map[string]any{"duration": "1h"})},
		},
		TimeMapsPage: "Time Maps",
		Location:     london,
	})
	// Missing Time Maps page is reported, but the graph default still applies.
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Tasks) != 1 {
		t.Fatalf("Tasks = %+v, want one", res.Tasks)
	}
	got := res.Tasks[0]
	if got.Graph != "work" || got.GraphOrder != 0 {
		t.Fatalf("Graph = %q order %d, want work/0", got.Graph, got.GraphOrder)
	}
	if len(got.TimeMaps) != 1 || got.TimeMaps[0] != "deep-work" {
		t.Fatalf("TimeMaps = %v, want [deep-work] from graph default", got.TimeMaps)
	}
}

func TestExplicitTimeMapWinsOverGraphDefault(t *testing.T) {
	res, err := resolve.Resolve(resolve.Input{
		Graph:          "work",
		DefaultTimeMap: "deep-work",
		Pages:          []*logseq.Page{page(day)},
		Trees: map[string][]*logseq.Block{
			day: {props(marked("TODO", id1, "TODO Do the work"), map[string]any{"duration": "1h", "time-map": "errands"})},
		},
		TimeMapsPage: "Time Maps",
		Location:     london,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Tasks) != 1 || len(res.Tasks[0].TimeMaps) != 1 || res.Tasks[0].TimeMaps[0] != "errands" {
		t.Fatalf("Tasks = %+v, want explicit [errands]", res.Tasks)
	}
}

func TestGraphPrefixInIssues(t *testing.T) {
	res, err := resolve.Resolve(resolve.Input{
		Graph:      "family",
		GraphOrder: 1,
		Pages:      []*logseq.Page{page(day)},
		Trees: map[string][]*logseq.Block{
			day: {marked("TODO", id1, "TODO No duration here")},
		},
		TimeMapsPage: "Time Maps",
		Location:     london,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Issues) == 0 {
		t.Fatal("want at least the missing-duration issue")
	}
	found := false
	for _, is := range res.Issues {
		if is.Kind == resolve.IssueMissingDuration {
			found = true
			if len(is.Context) < 13 || is.Context[:13] != "graph family," {
				t.Fatalf("Context = %q, want graph family prefix", is.Context)
			}
		}
	}
	if !found {
		t.Fatalf("Issues = %+v, want missing-duration", res.Issues)
	}
}
