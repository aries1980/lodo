package resolve_test

import (
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/resolve"
)

var london = mustLocation("Europe/London")

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// Fixture helpers build payloads shaped as the running app returned them to
// the research probe (see internal/logseq/testdata): pages with parsed
// properties, block trees with markers, raw content and property maps.

func page(name string, props ...map[string]any) *logseq.Page {
	p := &logseq.Page{Name: name, UUID: name + "-page-uuid"}
	if len(props) > 0 {
		p.Properties = props[0]
	}
	return p
}

func block(uuid, content string, children ...*logseq.Block) *logseq.Block {
	return &logseq.Block{UUID: uuid, Content: content, Children: children}
}

func marked(marker, uuid, content string, children ...*logseq.Block) *logseq.Block {
	b := block(uuid, content, children...)
	b.Marker = marker
	return b
}

// props attaches parsed properties to a block.
func props(b *logseq.Block, kv map[string]any) *logseq.Block {
	b.Properties = kv
	return b
}

// fixture resolves one graph exactly as the reader hands it over, with the
// default Time Maps page behind the scenes when the case does not build one
// (so task tables stay about tasks).
func fixture(pages []*logseq.Page, trees map[string][]*logseq.Block) (resolve.Result, error) {
	hasTm := false
	for _, p := range pages {
		if p.Name == "Time Maps" {
			hasTm = true
		}
	}
	for key := range trees {
		if key == "Time Maps" {
			hasTm = true
		}
	}
	if !hasTm {
		pages = append(pages, page("Time Maps", nil))
		trees["Time Maps"] = []*logseq.Block{
			props(block("default-map-uuid", "Deep Work"), map[string]any{"default": true}),
			block("weekends-map-uuid", "Weekends"),
		}
	}
	return fixtureScene(pages, trees)
}

// fixtureScene resolves exactly what it is handed over.
func fixtureScene(pages []*logseq.Page, trees map[string][]*logseq.Block) (resolve.Result, error) {
	return resolve.Resolve(resolve.Input{
		Pages:        pages,
		Trees:        trees,
		TimeMapsPage: "Time Maps",
		Location:     london,
	})
}

func taskIDs(res resolve.Result) []string {
	var out []string
	for _, t := range res.Tasks {
		out = append(out, t.ID)
	}
	return out
}

// looseIssues compares issue kinds only, so tables spell one expectation per
// kind.
func looseIssues(kinds ...string) []resolve.Issue {
	out := make([]resolve.Issue, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, resolve.Issue{Kind: k})
	}
	return out
}

// ignoreIssueWords hides Context and Detail: tests below assert kinds.
var ignoreIssueWords = cmpopts.IgnoreFields(resolve.Issue{}, "Context", "Detail")

const day = "2026-10-15"

const (
	id1 = "11111111-1111-4111-8111-111111111111"
	id2 = "22222222-2222-4222-8222-222222222222"
	id3 = "33333333-3333-4333-8333-333333333333"
	id4 = "44444444-4444-4444-8444-444444444444"
)

// TestTaskGate pins the ADR 0004 gate: TODO/DOING and an explicit
// duration:: are both required, with no default duration anywhere.
func TestTaskGate(t *testing.T) {
	tests := []struct {
		name    string
		blocks  []*logseq.Block
		wantIDs []string
		want    []resolve.Issue
	}{
		{
			"TODO with explicit duration is a Task",
			[]*logseq.Block{props(marked("TODO", id1, "TODO Draft the report"), map[string]any{"duration": "90m"})},
			[]string{id1},
			nil,
		},
		{
			"DOING with explicit duration is a Task",
			[]*logseq.Block{props(marked("DOING", id2, "DOING Review the draft"), map[string]any{"duration": "30m"})},
			[]string{id2},
			nil,
		},
		{
			"TODO without duration is a note, reported",
			[]*logseq.Block{marked("TODO", id3, "TODO Brainstorm ideas")},
			nil,
			looseIssues(resolve.IssueMissingDuration),
		},
		{
			"nested candidates are all Tasks, in outline order",
			[]*logseq.Block{
				props(marked("TODO", id1, "TODO Parent of the process", props(marked("TODO", id2, "TODO Child of the parent"), map[string]any{"duration": "30m"})), map[string]any{"duration": "1h"}),
				props(marked("TODO", id3, "TODO Next in line"), map[string]any{"duration": "30m"}),
			},
			[]string{id1, id2, id3},
			nil,
		},
		{
			"DONE and CANCELLED are complete, not Tasks and not issues",
			[]*logseq.Block{
				props(marked("DONE", id1, "DONE Tidy the garage"), map[string]any{"duration": "30m"}),
				props(marked("CANCELLED", id2, "CANCELLED Old idea"), map[string]any{"duration": "15m"}),
			},
			nil,
			nil,
		},
		{
			"plain blocks are notes, never flagged",
			[]*logseq.Block{block(id4, "A plain note about the week")},
			nil,
			nil,
		},
		{
			"a malformed duration is reported, never guessed",
			[]*logseq.Block{props(marked("TODO", id1, "TODO Vague effort"), map[string]any{"duration": "soon"})},
			nil,
			looseIssues(resolve.IssueBadProperty),
		},
		{
			"a typed numeric duration is malformed text",
			[]*logseq.Block{props(marked("TODO", id1, "TODO One hour of code review"), map[string]any{"duration": map[string]any{"hours": 1}})},
			nil,
			looseIssues(resolve.IssueBadProperty),
		},
		{
			"zero and negative durations fail the gate",
			[]*logseq.Block{
				props(marked("TODO", id1, "TODO Nothing"), map[string]any{"duration": "0m"}),
				props(marked("TODO", id2, "TODO Less than nothing"), map[string]any{"duration": "-5m"}),
			},
			nil,
			looseIssues(resolve.IssueBadProperty, resolve.IssueBadProperty),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := fixture([]*logseq.Page{page(day)}, map[string][]*logseq.Block{day: tt.blocks})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if diff := cmp.Diff(tt.wantIDs, taskIDs(res)); diff != "" {
				t.Fatalf("Task IDs mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.want, res.Issues, ignoreIssueWords); diff != "" {
				t.Fatalf("Issues mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestTaskProperties maps every property in tasks.md's table onto the
// model, slugs case-normalised and stamps resolved in the scheduling zone.
func TestTaskProperties(t *testing.T) {
	pinTime := time.Date(2026, 10, 20, 9, 30, 0, 0, london)

	res, err := fixture(
		[]*logseq.Page{page(day)},
		map[string][]*logseq.Block{day: {
			props(marked("TODO", id1, "TODO Draft the spec\nid:: "+id1),
				map[string]any{
					"duration":   "90m",
					"time-map":   "Deep-Work, weekends",
					"priority":   "b",
					"deadline":   "<2026-10-25 Sun 12:00>",
					"plan-start": "2026-10-18",
					"plan-end":   "2026-10-24",
				}),
			props(marked("DOING", id2, "DOING Ship the thing"),
				map[string]any{
					"duration":  "45m",
					"scheduled": "<2026-10-20 Tue 09:30>",
				}),
		}},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Tasks) != 2 {
		t.Fatalf("got %d Tasks, want 2: %+v", len(res.Tasks), res.Tasks)
	}

	start := model.CivilDate{Year: 2026, Month: time.October, Day: 18}
	end := model.CivilDate{Year: 2026, Month: time.October, Day: 24}
	deadline := model.CivilDate{Year: 2026, Month: time.October, Day: 25}

	want := []model.Task{
		{
			ID: id1, Text: "Draft the spec", Marker: model.MarkerTODO,
			Duration: 90 * time.Minute,
			TimeMaps: []string{"deep-work", "weekends"},
			Priority: model.PriorityB,
			Deadline: &deadline, PlanStart: &start, PlanEnd: &end,
			OutlineOrder: 0,
		},
		{
			ID: id2, Text: "Ship the thing", Marker: model.MarkerDOING,
			Duration:     45 * time.Minute,
			Pin:          &pinTime,
			OutlineOrder: 1,
		},
	}
	if diff := cmp.Diff(want, res.Tasks); diff != "" {
		t.Fatalf("Tasks mismatch (-want +got):\n%s", diff)
	}
	if len(res.Issues) != 0 {
		t.Fatalf("Issues = %+v, want none", res.Issues)
	}
}

// TestTaskSnapshot: every resolved Task carries its Snapshot (M2), used by
// the store in M5.
func TestTaskSnapshot(t *testing.T) {
	end := model.CivilDate{Year: 2026, Month: time.October, Day: 24}
	res, err := fixture([]*logseq.Page{page(day)},
		map[string][]*logseq.Block{day: {props(marked("TODO", id1, "TODO Something"), map[string]any{
			"duration": "90m", "priority": "A", "plan-end": "2026-10-24",
		})}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	snap := res.Tasks[0].Snapshot()
	want := model.Snapshot{Text: "Something", Marker: model.MarkerTODO, Duration: 90 * time.Minute, Priority: model.PriorityA, PlanEnd: &end}
	if diff := cmp.Diff(want, snap); diff != "" {
		t.Fatalf("Snapshot mismatch (-want +got):\n%s", diff)
	}
}

func TestPinIsExactInSchedulingZone(t *testing.T) {
	res, err := fixture(
		[]*logseq.Page{page(day)},
		map[string][]*logseq.Block{day: {
			props(marked("TODO", id1, "TODO Morning standup\nid:: "+id1),
				map[string]any{"duration": "15m", "scheduled": "<2026-10-20 Tue 09:00>"}),
		}},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Tasks) != 1 || res.Tasks[0].Pin == nil {
		t.Fatalf("Tasks = %+v, want one with a Pin", res.Tasks)
	}
	want := time.Date(2026, 10, 20, 9, 0, 0, 0, london)
	if !res.Tasks[0].Pin.Equal(want) {
		t.Fatalf("Pin = %v, want %v (exactly as authored, in the scheduling zone)", *res.Tasks[0].Pin, want)
	}
	if len(res.Issues) != 0 {
		t.Fatalf("Issues = %+v, want none: id:: is known plumbing", res.Issues)
	}
}

// TestPermissiveKeys: keys compare permissively — case-insensitive, spaced.
func TestPermissiveKeys(t *testing.T) {
	res, err := fixture(
		[]*logseq.Page{page(day)},
		map[string][]*logseq.Block{day: {
			props(marked("TODO", id1, "TODO The assignment"),
				map[string]any{
					"Duration ": "45m",
					"TIME-MAP":  "deep-work",
					" Priority": "A",
				}),
		}},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Tasks) != 1 {
		t.Fatalf("Tasks = %+v, want one", res.Tasks)
	}
	got := res.Tasks[0]
	if got.Duration != 45*time.Minute || got.Priority != model.PriorityA ||
		len(got.TimeMaps) != 1 || got.TimeMaps[0] != "deep-work" {
		t.Fatalf("permissive keys did not resolve: %+v", got)
	}
	if len(res.Issues) != 0 {
		t.Fatalf("Issues = %+v, want none", res.Issues)
	}
}

func TestMissingBlockIDIsReportedAndSkipped(t *testing.T) {
	res, err := fixture(
		[]*logseq.Page{page(day)},
		map[string][]*logseq.Block{day: {marked("TODO", "", "TODO No identity")}},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if diff := cmp.Diff(looseIssues(resolve.IssueMissingBlockID), res.Issues, ignoreIssueWords); diff != "" {
		t.Fatalf("Issues mismatch (-want +got):\n%s", diff)
	}
	if len(res.Tasks) != 0 {
		t.Fatalf("Tasks = %+v, want none", res.Tasks)
	}
}

// TestInheritanceMatrix walks logseq-integration.md's table: time-map,
// priority, plan-start and plan-end inherit from the page; duration,
// deadline and scheduled never do; a block's own value always wins.
func TestInheritanceMatrix(t *testing.T) {
	tests := []struct {
		name      string
		pageKey   string
		pageValue string
		taskProps map[string]any // nil: the block carries nothing
		check     func(testing.TB, model.Task)
	}{
		{
			"time-map inherits", "time-map", "weekends", map[string]any{"duration": "1h"},
			func(tb testing.TB, task model.Task) {
				if len(task.TimeMaps) != 1 || task.TimeMaps[0] != "weekends" {
					tb.Fatalf("TimeMaps = %v, want [weekends]", task.TimeMaps)
				}
			},
		},
		{
			"priority inherits", "priority", "C", map[string]any{"duration": "1h"},
			func(tb testing.TB, task model.Task) {
				if task.Priority != model.PriorityC {
					tb.Fatalf("Priority = %q, want C", task.Priority)
				}
			},
		},
		{
			"plan-start inherits", "plan-start", "2026-10-17", map[string]any{"duration": "1h"},
			func(tb testing.TB, task model.Task) {
				if task.PlanStart == nil || task.PlanStart.Day != 17 {
					tb.Fatalf("PlanStart = %+v, want day 17", task.PlanStart)
				}
			},
		},
		{
			"plan-end inherits", "plan-end", "2026-10-28", map[string]any{"duration": "1h"},
			func(tb testing.TB, task model.Task) {
				if task.PlanEnd == nil || task.PlanEnd.Day != 28 {
					tb.Fatalf("PlanEnd = %+v, want day 28", task.PlanEnd)
				}
			},
		},
		{
			"deadline never inherits", "deadline", "2026-10-30", map[string]any{"duration": "1h"},
			func(tb testing.TB, task model.Task) {
				if task.Deadline != nil {
					tb.Fatalf("Deadline inherited: %+v", task.Deadline)
				}
			},
		},
		{
			"scheduled never inherits", "scheduled", "<2026-10-22 Thu 10:00>", map[string]any{"duration": "1h"},
			func(tb testing.TB, task model.Task) {
				if task.Pin != nil {
					tb.Fatalf("Pin inherited: %v", *task.Pin)
				}
			},
		},
		{
			"the block wins over the page, priority", "priority", "C",
			map[string]any{"duration": "1h", "priority": "A"},
			func(tb testing.TB, task model.Task) {
				if task.Priority != model.PriorityA {
					tb.Fatalf("Priority = %q, want the block's A", task.Priority)
				}
			},
		},
		{
			"the block wins over the page, time-map", "time-map", "weekends",
			map[string]any{"duration": "1h", "time-map": "deep-work"},
			func(tb testing.TB, task model.Task) {
				if len(task.TimeMaps) != 1 || task.TimeMaps[0] != "deep-work" {
					tb.Fatalf("TimeMaps = %v, want the block's [deep-work]", task.TimeMaps)
				}
			},
		},
		{
			"the block wins over the page, plan-end", "plan-end", "2026-10-28",
			map[string]any{"duration": "1h", "plan-end": "2026-10-21"},
			func(tb testing.TB, task model.Task) {
				if task.PlanEnd == nil || task.PlanEnd.Day != 21 {
					tb.Fatalf("PlanEnd = %+v, want the block's day 21", task.PlanEnd)
				}
			},
		},
		{
			"the block wins over the page, duration", "duration", "30m",
			map[string]any{"duration": "20m"},
			func(tb testing.TB, task model.Task) {
				if task.Duration != 20*time.Minute {
					tb.Fatalf("Duration = %v, want the block's 20m", task.Duration)
				}
			},
		},
		{
			"an empty block value falls back to the page", "priority", "B",
			map[string]any{"duration": "1h", "priority": ""},
			func(tb testing.TB, task model.Task) {
				if task.Priority != model.PriorityB {
					tb.Fatalf("Priority = %q, want the page's B", task.Priority)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := marked("TODO", id1, "TODO Do the work")
			if tt.taskProps != nil {
				b = props(b, tt.taskProps)
			}
			res, err := fixture(
				[]*logseq.Page{page(day, map[string]any{tt.pageKey: tt.pageValue})},
				map[string][]*logseq.Block{day: {b}},
			)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if tt.pageKey == "duration" && tt.taskProps == nil {
				// A page-level duration must not open the gate.
				if len(res.Tasks) != 0 {
					t.Fatalf("page duration:: leaked into the gate: Tasks = %+v", res.Tasks)
				}
				return
			}
			if len(res.Tasks) != 1 {
				t.Fatalf("Tasks = %+v, want one", res.Tasks)
			}
			tt.check(t, res.Tasks[0])
		})
	}
}

// TestOutlineOrder: depth-first, top-to-bottom display order of the block
// tree; complete blocks consume no order.
func TestOutlineOrder(t *testing.T) {
	res, err := fixture(
		[]*logseq.Page{page(day)},
		map[string][]*logseq.Block{day: {block("root", "The day",
			props(marked("TODO", "a-uuid", "TODO One"), map[string]any{"duration": "30m"}),
			props(marked("TODO", "c-uuid", "TODO Three (nested)", props(marked("TODO", "d-uuid", "TODO Four (nested deeper)"), map[string]any{"duration": "30m"})), map[string]any{"duration": "30m"}),
			props(marked("DONE", "x-uuid", "DONE Completed earlier"), map[string]any{"duration": "30m"}),
			props(marked("TODO", "b-uuid", "TODO Two"), map[string]any{"duration": "30m"}),
		)}},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Tasks) != 4 {
		t.Fatalf("Tasks = %+v, want 4", res.Tasks)
	}
	order := map[string]int{"a-uuid": 0, "c-uuid": 1, "d-uuid": 2, "b-uuid": 3}
	for _, task := range res.Tasks {
		if task.OutlineOrder != order[task.ID] {
			t.Fatalf("Task %s OutlineOrder = %d, want %d", task.ID, task.OutlineOrder, order[task.ID])
		}
	}
}

// TestTaskPagesSortDeterministically: pages in reverse enumeration order
// still resolve in name order, so outline order cannot depend on the app's
// page list.
func TestTaskPagesSortDeterministically(t *testing.T) {
	dayAfter := "2026-10-16"
	res, err := fixture(
		[]*logseq.Page{page(day), page(dayAfter)},
		map[string][]*logseq.Block{
			day:      {props(marked("TODO", id1, "TODO Earlier day"), map[string]any{"duration": "30m"})},
			dayAfter: {props(marked("TODO", id2, "TODO Later day"), map[string]any{"duration": "30m"})},
		},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Tasks) != 2 || res.Tasks[0].ID != id1 || res.Tasks[1].ID != id2 {
		t.Fatalf("Tasks = %+v, want %s before %s", res.Tasks, id1, id2)
	}
	if res.Tasks[1].OutlineOrder != 1 {
		t.Fatalf("OutlineOrder = %d, want 1", res.Tasks[1].OutlineOrder)
	}
}

// TestTimeMaps covers the ADR 0003 schema and the default-map rule.
func TestTimeMaps(t *testing.T) {
	res, err := fixture(
		[]*logseq.Page{page("Time Maps")},
		map[string][]*logseq.Block{"Time Maps": {
			props(block("m1", "Deep Work"), map[string]any{"default": true}),
			block("m2",
				"Errands",
				block("w1", "Sat 10:00-13:00"),
			),
			block("m3",
				"Weekends",
				block("w2", "Sat-Sun 08:00-20:00 yellow"),
			),
		}},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	want := []model.TimeMap{
		{Slug: "deep-work", Default: true},
		{Slug: "errands", Windows: []model.Window{{
			Days: []time.Weekday{time.Saturday}, StartMinute: 600, EndMinute: 780, Tier: model.TierGreen,
		}}},
		{Slug: "weekends", Windows: []model.Window{{
			Days: []time.Weekday{time.Saturday, time.Sunday}, StartMinute: 480, EndMinute: 1200, Tier: model.TierYellow,
		}}},
	}
	if diff := cmp.Diff(want, res.TimeMaps); diff != "" {
		t.Fatalf("TimeMaps mismatch (-want +got):\n%s", diff)
	}
	if len(res.Issues) != 0 {
		t.Fatalf("Issues = %+v, want none", res.Issues)
	}
}

func TestTimeMapNamesSlugifyCaseInsensitively(t *testing.T) {
	res, err := fixture(
		[]*logseq.Page{page("Time Maps")},
		map[string][]*logseq.Block{"time maps": { // the page name matches case-insensitively
			block("m1", "Deep Work"),
		}},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.TimeMaps) != 1 || res.TimeMaps[0].Slug != "deep-work" {
		t.Fatalf("TimeMaps = %+v, want one map deep-work", res.TimeMaps)
	}
}

func TestZeroDefaultIsAnIssue(t *testing.T) {
	res, err := fixture(
		[]*logseq.Page{page("Time Maps")},
		map[string][]*logseq.Block{"Time Maps": {block("m1", "Deep Work")}},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if diff := cmp.Diff(looseIssues(resolve.IssueDefaultMapCount), res.Issues, ignoreIssueWords); diff != "" {
		t.Fatalf("Issues mismatch (-want +got):\n%s", diff)
	}
	if res.TimeMaps[0].Default {
		t.Fatal("no default:: true means no map is the default")
	}
}

func TestMultipleDefaultsAreAnIssueAndApplyToNone(t *testing.T) {
	res, err := fixture(
		[]*logseq.Page{page("Time Maps")},
		map[string][]*logseq.Block{"Time Maps": {
			props(block("m1", "Deep Work"), map[string]any{"default": true}),
			props(block("m2", "Errands"), map[string]any{"default": true}),
			block("m3", "Weekends"),
		}},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if diff := cmp.Diff(looseIssues(resolve.IssueDefaultMapCount), res.Issues, ignoreIssueWords); diff != "" {
		t.Fatalf("Issues mismatch (-want +got):\n%s", diff)
	}
	for _, m := range res.TimeMaps {
		if m.Default {
			t.Fatalf("map %s kept Default=true although more than one applies; that would be a guess", m.Slug)
		}
	}
}

func TestBadDefaultFlagIsAnIssue(t *testing.T) {
	res, err := fixture(
		[]*logseq.Page{page("Time Maps")},
		map[string][]*logseq.Block{"Time Maps": {
			props(block("m1", "Deep Work"), map[string]any{"default": "yes"}),
		}},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if diff := cmp.Diff(looseIssues(resolve.IssueBadDefault, resolve.IssueDefaultMapCount), res.Issues, ignoreIssueWords); diff != "" {
		t.Fatalf("Issues mismatch (-want +got):\n%s", diff)
	}
	if len(res.TimeMaps) != 1 || res.TimeMaps[0].Default {
		t.Fatalf("TimeMaps = %+v, want the map kept with Default=false", res.TimeMaps)
	}
}

func TestMissingTimeMapsPageIsAnIssue(t *testing.T) {
	// A missing Time Maps page is one issue; the default-map rule would only
	// double up on it.
	res, err := fixtureScene(
		[]*logseq.Page{page(day)},
		map[string][]*logseq.Block{day: {}},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if diff := cmp.Diff(looseIssues(resolve.IssueMissingTimeMapsPage), res.Issues, ignoreIssueWords); diff != "" {
		t.Fatalf("Issues mismatch (-want +got):\n%s", diff)
	}
}

func TestDuplicateMapSlugIsAnIssue(t *testing.T) {
	res, err := fixture(
		[]*logseq.Page{page("Time Maps")},
		map[string][]*logseq.Block{"Time Maps": {
			block("m1", "Deep Work"),
			block("m2", "deep work"),
			props(block("m3", "Deep Work"), map[string]any{"default": true}),
		}},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.TimeMaps) != 1 || res.TimeMaps[0].Slug != "deep-work" {
		t.Fatalf("TimeMaps = %+v, want only the first deep-work, without the stripped default", res.TimeMaps)
	}
	// The skipped default:: map does not make the kept map default.
	// The default:: map is itself a duplicate, so it is skipped too: which
	// leaves zero defaults — reported.
	if diff := cmp.Diff(
		looseIssues(resolve.IssueDuplicateMap, resolve.IssueDuplicateMap, resolve.IssueDefaultMapCount),
		res.Issues, ignoreIssueWords); diff != "" {
		t.Fatalf("Issues mismatch (-want +got):\n%s", diff)
	}
}

func TestBadWindowLineIsAnIssue(t *testing.T) {
	res, err := fixture(
		[]*logseq.Page{page("Time Maps")},
		map[string][]*logseq.Block{"Time Maps": {
			block("m1",
				"Deep Work",
				block("w1", "Mon-Fri 09:00-11:00"),
				block("w2", "evenings whenever"),
				block("w3", "Sat-Sun 00:30-00:15"), // a window crossing past itself
			),
			props(block("d1", "Everyday"), map[string]any{"default": true}), // the default rule satisfied
		}},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.TimeMaps) != 2 || len(res.TimeMaps[0].Windows) != 1 {
		t.Fatalf("windows = %+v, want deep-work keeping the one interpretable line", res.TimeMaps)
	}
	if !res.TimeMaps[1].Default || len(res.TimeMaps[1].Windows) != 0 {
		t.Fatalf("Everyday = %+v, want the default map with no windows", res.TimeMaps[1])
	}
	if diff := cmp.Diff(looseIssues(resolve.IssueBadWindow, resolve.IssueBadWindow), res.Issues, ignoreIssueWords); diff != "" {
		t.Fatalf("Issues mismatch (-want +got):\n%s", diff)
	}
}

func TestMapNameWithoutSlugifiableTextIsAnIssue(t *testing.T) {
	res, err := fixture(
		[]*logseq.Page{page("Time Maps")},
		map[string][]*logseq.Block{"Time Maps": {block("m1", "***")}},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.TimeMaps) != 0 {
		t.Fatalf("TimeMaps = %+v, want none", res.TimeMaps)
	}
	if diff := cmp.Diff(looseIssues(resolve.IssueBadTimeMap, resolve.IssueDefaultMapCount), res.Issues, ignoreIssueWords); diff != "" {
		t.Fatalf("Issues mismatch (-want +got):\n%s", diff)
	}
}

func TestUnknownTimeMapSlugIsAnIssue(t *testing.T) {
	res, err := fixture(
		[]*logseq.Page{page(day), page("Time Maps")},
		map[string][]*logseq.Block{
			day: {props(marked("TODO", id1, "TODO The thing"), map[string]any{
				"duration": "1h", "time-map": "deep-wro, Deep-Work",
			})},
			"Time Maps": {props(block("m1", "Deep Work"), map[string]any{"default": true})},
		},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Tasks) != 1 || len(res.Tasks[0].TimeMaps) != 2 {
		t.Fatalf("Tasks = %+v, want the task with both slugs kept", res.Tasks)
	}
	if diff := cmp.Diff(looseIssues(resolve.IssueUnknownTimeMap), res.Issues, ignoreIssueWords); diff != "" {
		t.Fatalf("Issues mismatch (-want +got):\n%s", diff)
	}
}

// TestBadPropertyValues: every malformed value is reported and dropped; the
// Task keeps the rest of its inputs.
func TestBadPropertyValues(t *testing.T) {
	tests := []struct {
		name  string
		props map[string]any
		check func(t *testing.T, task model.Task)
	}{
		{
			"priority junk",
			map[string]any{"duration": "1h", "priority": "D"},
			func(t *testing.T, task model.Task) {
				if task.Priority != "" {
					t.Fatalf("Priority = %q, want the dropped zero value", task.Priority)
				}
			},
		},
		{
			"deadline garbage",
			map[string]any{"duration": "1h", "deadline": "next week"},
			func(t *testing.T, task model.Task) {
				if task.Deadline != nil {
					t.Fatal("Deadline was guessed from garbage")
				}
			},
		},
		{
			"plan-start with a time",
			map[string]any{"duration": "1h", "plan-start": "<2026-10-20 Tue 09:00>"},
			func(t *testing.T, task model.Task) {
				if task.PlanStart != nil {
					t.Fatal("a time-stamped plan-start must not become a day")
				}
			},
		},
		{
			"scheduled without a time",
			map[string]any{"duration": "1h", "scheduled": "<2026-10-20 Tue>"},
			func(t *testing.T, task model.Task) {
				if task.Pin != nil {
					t.Fatal("a day-only scheduled:: must not become a Pin")
				}
			},
		},
		{
			"scheduled garbage",
			map[string]any{"duration": "1h", "scheduled": "friday soon"},
			func(t *testing.T, task model.Task) {
				if task.Pin != nil {
					t.Fatal("Pin guessed from garbage")
				}
			},
		},
		{
			"unknown property on a task-marked block",
			map[string]any{"duration": "1h", "effort": "3"},
			func(t *testing.T, task model.Task) {
				if task.Duration != time.Hour {
					t.Fatal("unknown property interfered with resolution")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := fixture([]*logseq.Page{page(day)},
				map[string][]*logseq.Block{day: {props(marked("TODO", id1, "TODO The work"), tt.props)}})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if len(res.Tasks) != 1 {
				t.Fatalf("Tasks = %+v, want the Task kept with its good properties; issues %+v", res.Tasks, res.Issues)
			}
			tt.check(t, res.Tasks[0])
		})
	}
}

// TestIssueContexts points at pages and blocks: the report must identify
// where each problem came from.
func TestIssueContexts(t *testing.T) {
	res, err := fixture(
		[]*logseq.Page{page(day)},
		map[string][]*logseq.Block{day: {marked("TODO", id1, "TODO Undated effort")}},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Issues) != 1 {
		t.Fatalf("Issues = %+v, want one", res.Issues)
	}
	got := res.Issues[0]
	if got.Kind != resolve.IssueMissingDuration {
		t.Fatalf("Kind = %q", got.Kind)
	}
	if got.Context == "" || !blockIsNamed(got.Context, day, id1) {
		t.Fatalf("Context = %q, want the page and the block id", got.Context)
	}
	if got.Detail == "" {
		t.Fatal("Detail empty: the omission should be explained")
	}
}

func blockIsNamed(context, pageName, blockID string) bool {
	return containsWord(context, pageName) && containsWord(context, blockID)
}

func containsWord(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
