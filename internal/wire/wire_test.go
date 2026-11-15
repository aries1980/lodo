package wire

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aries1980/lodo/internal/jsonx"
	"github.com/aries1980/lodo/internal/planner"
)

var (
	_ Payload = Check{}
	_ Payload = Forecast{}
	_ Payload = Schedule{}
	_ Payload = Refresh{}
	_ Payload = Explain{}
	_ Payload = ExplainError{}
	_ Payload = Auth{}
)

func TestPayloadsCarrySchemaVersion(t *testing.T) {
	payloads := []struct {
		name    string
		payload Payload
		want    int
	}{
		{"check", Check{SchemaVersion: CheckVersion}, CheckVersion},
		{"forecast", Forecast{SchemaVersion: ForecastVersion}, ForecastVersion},
		{"schedule", Schedule{SchemaVersion: ScheduleVersion}, ScheduleVersion},
		{"refresh", Refresh{SchemaVersion: RefreshVersion}, RefreshVersion},
		{"explain", Explain{SchemaVersion: ExplainVersion}, ExplainVersion},
		{"explain error", ExplainError{SchemaVersion: ExplainVersion}, ExplainVersion},
		{"auth", Auth{SchemaVersion: AuthVersion}, AuthVersion},
	}
	for _, tt := range payloads {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := jsonx.Marshal(tt.payload)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			want := `"schema_version":` + strconv.Itoa(tt.want)
			if !strings.Contains(string(raw), want) {
				t.Fatalf("payload missing %s: %s", want, raw)
			}
		})
	}
}

// TestPayloadsMarshalWithoutDomainLeaks pins the whole surface through the
// encoder commands use. A time.Duration, an untagged Snapshot, or a Go-cased
// field name would fail or leak here before it reaches a consumer.
func TestPayloadsMarshalWithoutDomainLeaks(t *testing.T) {
	now := time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)
	end := now.Add(30 * time.Minute)
	payloads := []Payload{
		Check{
			SchemaVersion: CheckVersion,
			GraphIssues:   []Issue{{Kind: "todo-missing-duration", Context: "page", Detail: "detail"}},
			Shortfalls:    []Shortfall{{TaskID: "t1", Task: "Write", Required: 2, BlockedBy: []string{"dep"}}},
			StoodDown:     []StoodDown{{TaskID: "t1", Task: "Write", BlockIndex: 1, Slots: 2}},
			Orphaned:      []Orphan{{EventID: "e1", TaskID: "t1", Task: "Write", Block: 0}},
			Explanations:  []Explanation{{TaskID: "t1", BlockIndex: 0, Kind: "flexible", Start: now, Slots: 2}},
			Busy:          &Busy{WriteCalendar: "primary", Sources: []BusySource{{CalendarID: "primary", Status: "read", Events: 1}}},
		},
		Forecast{
			SchemaVersion: ForecastVersion,
			Blocks:        []Block{{TaskID: "t1", Task: "Write", Index: 0, Start: now, End: end, Slots: 2, Kind: "flexible"}},
			Managed:       []Managed{{ID: "e1", Summary: "Write"}},
			Busy:          Busy{WriteCalendar: "primary"},
		},
		Schedule{
			SchemaVersion: ScheduleVersion,
			Writes:        []Write{{Op: "insert", EventID: "e1", TaskID: "t1", Task: "Write", Start: now, End: end}},
			Reconciled:    &Reconcile{BindingsUpserted: 1},
			Dispositions:  []Disposition{{TaskID: "t1", Task: "Write", BlockIndex: 0, Kind: "pinned", Start: now, End: end, Slots: 2}},
			Busy:          Busy{WriteCalendar: "primary"},
		},
		Refresh{
			SchemaVersion: RefreshVersion,
			Adopted:       []Binding{{TaskID: "t1", EventID: "e1", Start: now, End: end, Slots: 2}},
			Captured:      []Disposition{{TaskID: "t1", BlockIndex: 0, Kind: "stood-down", Slots: 2}},
			Conflicts:     []Conflict{{TaskID: "t1", EventID: "e1"}},
			Busy:          Busy{WriteCalendar: "primary"},
		},
		Explain{
			SchemaVersion: ExplainVersion,
			Context:       ExplainContext{Command: "explain", Now: now, Timezone: "UTC"},
			Task:          ExplainTask{ID: "t1", Text: "Write", Marker: "TODO", Duration: "30m", Pin: &now},
			Eligibility:   planner.Eligibility{RequiredSlots: 2, Considered: true},
			Ordering:      planner.Ordering{Kind: "flexible", Position: 1, Total: 1},
			Properties:    []Provenance{{Property: "duration", Value: "30m", Source: "direct"}},
			Dependency:    planner.Dependency{Depends: []string{"dep"}},
			Window:        planner.Window{From: now, To: end},
			TimeMaps:      planner.TimeMaps{Requested: []string{"deep-work"}},
			Placement:     &planner.Placement{Floor: 2, RequiredSlots: 2},
			Blocks:        []planner.BlockDecision{{Index: 0, Kind: "flexible", Start: now, End: end, Slots: 2}},
			Outcome:       planner.Outcome{Required: 2, Placed: 2},
			Dispositions:  []Disposition{{TaskID: "t1", Task: "Write", BlockIndex: 0, Kind: "pinned", Start: now, End: end, Slots: 2}},
			Spillover:     &Spillover{PrimaryPlaced: 2, Summary: "primary"},
			Issues:        []Issue{{Kind: "unknown-dependency", Context: "page", Detail: "detail"}},
			Report:        &Shortfall{TaskID: "t1", Task: "Write", Reason: "short"},
			Busy:          &Busy{WriteCalendar: "primary"},
		},
		ExplainError{
			SchemaVersion: ExplainVersion,
			Error:         "ambiguous",
			Query:         "write",
			Matches:       []TaskMatch{{ID: "t1", Text: "Write"}},
		},
		Auth{SchemaVersion: AuthVersion, Stored: "/tmp/credentials.json"},
	}
	for _, payload := range payloads {
		raw, err := jsonx.Marshal(payload)
		if err != nil {
			t.Fatalf("%T: marshal: %v", payload, err)
		}
		for _, leak := range []string{"Snapshot", "Duration", "TaskID", "BlockIndex"} {
			if strings.Contains(string(raw), leak) {
				t.Fatalf("%T leaks %q: %s", payload, leak, raw)
			}
		}
	}
}

func TestTaskMatchOmitsEmptyGraph(t *testing.T) {
	raw, err := jsonx.Marshal(TaskMatch{ID: "t1", Text: "Write"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "graph") {
		t.Fatalf("empty graph must be omitted: %s", raw)
	}
}
