package run_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/google/go-cmp/cmp"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/planner"
	"github.com/aries1980/lodo/internal/run"
)

var london = mustLocation("Europe/London")

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// at returns an instant in the fixture week: Monday 2026-11-09, safely
// after the October DST transition, like M2's tests.
func at(day int, hour, min int) time.Time {
	return time.Date(2026, time.November, day, hour, min, 0, 0, london)
}

// stubClient is the graph as the app would hand it over — Go literals, no
// network.
type stubClient struct {
	pages []*logseq.Page
	trees map[string][]*logseq.Block
	props map[string]*logseq.Page

	pageCalls []string
}

func newStubClient(graph graphFixture) *stubClient {
	c := &stubClient{
		trees: graph.trees,
		props: map[string]*logseq.Page{},
	}
	for _, p := range graph.pages {
		c.pages = append(c.pages, &logseq.Page{Name: p.Name})
	}
	return c
}

func (c *stubClient) Pages(context.Context) ([]*logseq.Page, error) {
	out := make([]*logseq.Page, len(c.pages))
	for i, p := range c.pages {
		out[i] = &logseq.Page{Name: p.Name}
	}
	return out, nil
}

func (c *stubClient) Page(_ context.Context, name string) (*logseq.Page, error) {
	c.pageCalls = append(c.pageCalls, name)
	if info, ok := c.props[name]; ok {
		return info, nil
	}
	return &logseq.Page{Name: name, Properties: nil}, nil
}

func (c *stubClient) Tree(_ context.Context, name string) ([]*logseq.Block, error) {
	tree, ok := c.trees[name]
	if !ok {
		return nil, errors.New("no such page: " + name)
	}
	return tree, nil
}

type graphFixture struct {
	pages []*logseq.Page
	trees map[string][]*logseq.Block
}

func block(uuid, content string, children ...*logseq.Block) *logseq.Block {
	return &logseq.Block{UUID: uuid, Content: content, Children: children}
}

// mapDeepWork is the default Time Map block with its window children.
var mapDeepWork = props(block("map-deep", "Deep Work", block("w-deep1", "Mon-Fri 09:00-12:00")),
	map[string]any{"default": true})

func marked(marker, uuid, content string, children ...*logseq.Block) *logseq.Block {
	b := block(uuid, content, children...)
	b.Marker = marker
	return b
}

func props(b *logseq.Block, kv map[string]any) *logseq.Block {
	b.Properties = kv
	return b
}

func memPage(name string) *logseq.Page {
	return &logseq.Page{Name: name}
}

// fixtureWeek is a small but loaded week: a Pin, plan windows, priority
// ties, a Time Map with a default, and a Task that cannot fully fit.
func fixtureWeek() graphFixture {
	return graphFixture{
		pages: []*logseq.Page{
			memPage("2026-11-09"),
			memPage("projects"),
			memPage("Time Maps"),
		},
		trees: map[string][]*logseq.Block{
			"Time Maps": {
				mapDeepWork,
				block("map-errands", "Errands",
					block("w-err", "Sat 10:00-13:00"),
				),
			},
			"2026-11-09": {
				props(marked("TODO", "t-audit", "TODO Audit the ledger"),
					map[string]any{"duration": "4h", "plan-end": "2026-11-09"}),
				props(marked("TODO", "t-invoices", "TODO Print invoices"),
					map[string]any{"duration": "30m", "plan-end": "2026-11-11"}),
				props(marked("TODO", "t-pin", "TODO Team call"),
					map[string]any{"duration": "30m", "scheduled": "<2026-11-10 Tue 09:30>"}),
				props(marked("TODO", "t-draft", "TODO Draft the field notes"),
					map[string]any{"duration": "1h", "priority": "B"}),
				props(marked("TODO", "t-review", "TODO Explain the budget line by line"),
					map[string]any{"duration": "30m", "priority": "B"}),
				props(marked("TODO", "t-errands", "TODO Buy groceries"),
					map[string]any{"duration": "2h", "time-map": "errands"}),
				props(marked("TODO", "t-remote", "TODO Faraway planning"),
					map[string]any{"duration": "1h", "plan-start": "2026-11-20"}),
				marked("TODO", "t-note", "TODO Brainstorm what to do with the garden"),
			},
			"projects": {
				block("p-1", "Plain project notes, no Tasks here"),
			},
		},
	}
}

func testConfig() config.Config {
	return config.Config{
		Logseq: config.Logseq{
			APIURL:       "http://127.0.0.1:12315/api",
			TimeMapsPage: "Time Maps",
		},
		Scheduling: config.Scheduling{
			Granularity: "15m",
			WindowDays:  7,
			Timezone:    "Europe/London",
		},
		Calendar: config.Calendar{Provider: "google", ID: "primary"},
	}
}

func TestGoldenForecast(t *testing.T) {
	client := newStubClient(fixtureWeek())
	ctx := context.Background()

	outcome, err := run.Graph(ctx, client, nil, testConfig(), at(9, 8, 0))
	if err != nil {
		t.Fatalf("Graph: %v", err)
	}

	pin := at(10, 9, 30)

	want := planner.Schedule{
		Blocks: []model.Block{
			{TaskID: "t-audit", Index: 0, Start: at(9, 9, 0), End: at(9, 12, 0), Slots: 12, Kind: model.BlockFlexible},
			{TaskID: "t-invoices", Index: 0, Start: at(10, 9, 0), End: at(10, 9, 30), Slots: 2, Kind: model.BlockFlexible},
			{TaskID: "t-pin", Index: 0, Start: pin, End: pin.Add(30 * time.Minute), Slots: 2, Kind: model.BlockPin},
			{TaskID: "t-draft", Index: 0, Start: at(10, 10, 0), End: at(10, 11, 0), Slots: 4, Kind: model.BlockFlexible},
			{TaskID: "t-review", Index: 0, Start: at(10, 11, 0), End: at(10, 11, 30), Slots: 2, Kind: model.BlockFlexible},
			{TaskID: "t-errands", Index: 0, Start: at(14, 10, 0), End: at(14, 12, 0), Slots: 8, Kind: model.BlockFlexible},
		},
		Report: planner.Report{
			Tasks: []planner.TaskReport{
				{
					TaskID:   "t-audit",
					Required: 16, Placed: 12, StoodDown: 0, Shortfall: 4,
					Reason: planner.ReasonPartialFit,
				},
			},
		},
		Explanations: []planner.Explanation{
			{TaskID: "t-audit", BlockIndex: 0, Kind: model.BlockFlexible, Start: at(9, 9, 0), Slots: 12, RunStart: at(9, 9, 0), RunSlots: 12},
			{TaskID: "t-invoices", BlockIndex: 0, Kind: model.BlockFlexible, Start: at(10, 9, 0), Slots: 2, RunStart: at(10, 9, 0), RunSlots: 2},
			{TaskID: "t-pin", BlockIndex: 0, Kind: model.BlockPin, Start: pin, Slots: 2},
			{TaskID: "t-draft", BlockIndex: 0, Kind: model.BlockFlexible, Start: at(10, 10, 0), Slots: 4, RunStart: at(10, 10, 0), RunSlots: 8},
			{TaskID: "t-review", BlockIndex: 0, Kind: model.BlockFlexible, Start: at(10, 11, 0), Slots: 2, RunStart: at(10, 11, 0), RunSlots: 4},
			{TaskID: "t-errands", BlockIndex: 0, Kind: model.BlockFlexible, Start: at(14, 10, 0), Slots: 8, RunStart: at(14, 10, 0), RunSlots: 12},
		},
	}
	if diff := cmp.Diff(want, outcome.Schedule); diff != "" {
		t.Fatalf("Schedule mismatch (-want +got):\n%s", diff)
	}

	// The beyond-horizon Task is silently unplaced; the TODO without a
	// duration is a check issue, never a silent skip.
	for _, b := range outcome.Schedule.Blocks {
		if b.TaskID == "t-remote" {
			t.Fatalf("a beyond-horizon Task got a Block: %+v", b)
		}
	}
	if diff := cmp.Diff([]string{"todo-missing-duration"}, issueKinds(outcome)); diff != "" {
		t.Fatalf("Issues mismatch (-want +got):\n%s", diff)
	}

	// The planner saw the Tasks in outline order; the run's own text lookup
	// answers by id.
	if got := run.TaskText(outcome.Tasks, "t-pin"); got != "Team call" {
		t.Fatalf("TaskText = %q, want the pinned Task's text", got)
	}
	if got := run.TaskText(outcome.Tasks, "no-such-id"); got != "" {
		t.Fatalf("TaskText(unknown) = %q, want empty", got)
	}
}

func issueKinds(outcome run.Outcome) []string {
	var out []string
	for _, i := range outcome.Issues {
		out = append(out, i.Kind)
	}
	return out
}

// stubBusy is the calendar as a test hands it in: one mask, the Managed
// events deliberately left out of it, or an error.
type stubBusy struct {
	mask    grid.Mask
	managed []calendar.Event
	err     error
}

func (s stubBusy) Busy(context.Context, *grid.Grid, time.Time) (grid.Mask, []calendar.Event, error) {
	return s.mask, s.managed, s.err
}

// busyGraph is the smallest useful graph: one 30m Task inside the default
// map, nothing else.
func busyGraph() graphFixture {
	return graphFixture{
		pages: []*logseq.Page{memPage("2026-11-09"), memPage("Time Maps")},
		trees: map[string][]*logseq.Block{
			"Time Maps": {mapDeepWork},
			"2026-11-09": {
				props(marked("TODO", "t-audit", "TODO Audit the ledger"), map[string]any{"duration": "30m"}),
			},
		},
	}
}

func TestBusyShapesForecastAndKeepsManagedAside(t *testing.T) {
	cfg := testConfig()
	now := at(9, 8, 0)
	g := grid.Build(now, london, 15*time.Minute, cfg.Scheduling.WindowDays)
	// An external meeting owns Monday 09:00-11:00, right where the default
	// map opens.
	busy := g.Overlapping(at(9, 9, 0), at(9, 11, 0))

	outcome, err := run.Graph(context.Background(), newStubClient(busyGraph()),
		stubBusy{mask: busy, managed: []calendar.Event{{ID: "m-1", Summary: "Lodo-made"}}}, cfg, now)
	if err != nil {
		t.Fatalf("Graph: %v", err)
	}

	want := planner.Schedule{
		Blocks: []model.Block{
			{TaskID: "t-audit", Index: 0, Start: at(9, 11, 0), End: at(9, 11, 30), Slots: 2, Kind: model.BlockFlexible},
		},
		Explanations: []planner.Explanation{
			{TaskID: "t-audit", BlockIndex: 0, Kind: model.BlockFlexible, Start: at(9, 11, 0), Slots: 2, RunStart: at(9, 11, 0), RunSlots: 4},
		},
	}
	if diff := cmp.Diff(want, outcome.Schedule); diff != "" {
		t.Fatalf("Schedule mismatch (-want +got):\n%s", diff)
	}
	if len(outcome.Managed) != 1 || outcome.Managed[0].ID != "m-1" {
		t.Fatalf("Managed = %+v, want the one handed in", outcome.Managed)
	}
}

func TestCalendarErrorsIsOperational(t *testing.T) {
	outcome, err := run.Graph(context.Background(), newStubClient(busyGraph()),
		stubBusy{err: errors.New("the calendar is unreachable")}, testConfig(), at(9, 8, 0))
	if err == nil || !strings.Contains(err.Error(), "the calendar is unreachable") {
		t.Fatalf("Graph err = %v, want the calendar's cause, not an empty mask", err)
	}
	if outcome.Schedule.Blocks != nil {
		t.Fatalf("a failed calendar must not half-plan: %+v", outcome.Schedule.Blocks)
	}
}
