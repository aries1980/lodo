package wire

import (
	"time"

	"github.com/aries1980/lodo/internal/planner"
)

// Schema versions. Payloads evolve independently; a change to a shared row
// bumps the version of every payload that contains it.
const (
	CheckVersion    = 1
	ForecastVersion = 1
	ScheduleVersion = 1
	RefreshVersion  = 1
	ExplainVersion  = 2
	AuthVersion     = 1
)

// BusySource is one read calendar's audit row.
type BusySource struct {
	CalendarID string `json:"calendar_id"`
	Status     string `json:"status"`
	Events     int    `json:"events"`
}

// Busy is the machine-readable busy audit: the write calendar plus each read
// calendar's status and the warnings the text prints as warning lines.
type Busy struct {
	WriteCalendar string       `json:"write_calendar"`
	WriteEvents   int          `json:"write_events"`
	Sources       []BusySource `json:"sources,omitempty"`
	Warnings      []string     `json:"warnings,omitempty"`
}

// Conflict is one shared-calendar overlap.
type Conflict struct {
	TaskID     string `json:"task_id,omitempty"`
	BlockIndex int    `json:"block_index"`
	EventID    string `json:"event_id"`
	Summary    string `json:"summary,omitempty"`
}

// Binding is one adopted or moved Task→Block→event projection.
type Binding struct {
	TaskID     string    `json:"task_id"`
	BlockIndex int       `json:"block_index"`
	EventID    string    `json:"event_id"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Slots      int       `json:"slots"`
}

// StoodDown is one Block the user deleted, as check reports it.
type StoodDown struct {
	TaskID     string `json:"task_id"`
	Task       string `json:"task"`
	Graph      string `json:"graph,omitempty"`
	BlockIndex int    `json:"block_index"`
	Slots      int    `json:"slots"`
}

// Managed is one Managed event forecast deliberately leaves out of the busy
// mask.
type Managed struct {
	ID      string `json:"id"`
	Summary string `json:"summary,omitempty"`
}

// Block is one scheduled interval with its Task display labels.
type Block struct {
	TaskID string    `json:"task_id"`
	Task   string    `json:"task"`
	Graph  string    `json:"graph,omitempty"`
	Index  int       `json:"index"`
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Slots  int       `json:"slots"`
	Kind   string    `json:"kind"`
}

// Shortfall is one Task the planner could not fully place.
type Shortfall struct {
	TaskID    string   `json:"task_id"`
	Task      string   `json:"task"`
	Graph     string   `json:"graph,omitempty"`
	Required  int      `json:"required"`
	Placed    int      `json:"placed"`
	StoodDown int      `json:"stood_down"`
	Shortfall int      `json:"shortfall"`
	Reason    string   `json:"reason"`
	BlockedBy []string `json:"blocked_by,omitempty"`
}

// Suggestion is one advisory placement suggestion.
type Suggestion struct {
	TaskID string `json:"task_id"`
	Task   string `json:"task"`
	Graph  string `json:"graph,omitempty"`
	Reason string `json:"reason"`
	Change string `json:"change"`
	Impact string `json:"impact"`
	Tier   string `json:"tier,omitempty"`
}

// Explanation is one placed Block's compact reason, with --verbose.
type Explanation struct {
	TaskID     string    `json:"task_id"`
	BlockIndex int       `json:"block_index"`
	Kind       string    `json:"kind"`
	Start      time.Time `json:"start"`
	Slots      int       `json:"slots"`
	RunStart   time.Time `json:"run_start,omitempty"`
	RunSlots   int       `json:"run_slots,omitempty"`
}

// Write is one calendar write a schedule run issued.
type Write struct {
	Op         string    `json:"op"`
	EventID    string    `json:"event_id"`
	TaskID     string    `json:"task_id,omitempty"`
	Task       string    `json:"task,omitempty"`
	Graph      string    `json:"graph,omitempty"`
	BlockIndex int       `json:"block_index,omitempty"`
	Start      time.Time `json:"start,omitempty"`
	End        time.Time `json:"end,omitempty"`
	Summary    string    `json:"summary,omitempty"`
}

// Orphan is one Managed event with no home in the plan.
type Orphan struct {
	EventID string `json:"event_id"`
	TaskID  string `json:"task_id,omitempty"`
	Task    string `json:"task,omitempty"`
	Graph   string `json:"graph,omitempty"`
	Block   int    `json:"block"`
	Summary string `json:"summary,omitempty"`
}

// Reconcile is the count summary of what a schedule run folded in.
type Reconcile struct {
	BindingsUpserted int `json:"bindings_upserted"`
	BindingsDeleted  int `json:"bindings_deleted"`
	Dispositions     int `json:"dispositions"`
}

// Disposition is one effective pinned or stood-down Block.
type Disposition struct {
	TaskID     string    `json:"task_id"`
	Task       string    `json:"task"`
	Graph      string    `json:"graph,omitempty"`
	BlockIndex int       `json:"block_index"`
	Kind       string    `json:"kind"`
	Start      time.Time `json:"start,omitempty"`
	End        time.Time `json:"end,omitempty"`
	Slots      int       `json:"slots,omitempty"`
}

// Issue is one graph problem reported by check.
type Issue struct {
	Kind    string `json:"kind"`
	Context string `json:"context"`
	Detail  string `json:"detail"`
}

// Provenance is one effective property and where it came from.
type Provenance struct {
	Property  string `json:"property"`
	Value     string `json:"value,omitempty"`
	Source    string `json:"source"`
	SourceID  string `json:"source_id,omitempty"`
	Inherited bool   `json:"inherited"`
}

// TaskMatch is one Task in an ambiguous explain query.
type TaskMatch struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Graph string `json:"graph,omitempty"`
}

// Check is the check command payload.
type Check struct {
	SchemaVersion int           `json:"schema_version"`
	GraphIssues   []Issue       `json:"graph_issues,omitempty"`
	Shortfalls    []Shortfall   `json:"shortfalls,omitempty"`
	Suggestions   []Suggestion  `json:"suggestions,omitempty"`
	StoodDown     []StoodDown   `json:"stood_down,omitempty"`
	Orphaned      []Orphan      `json:"orphaned,omitempty"`
	Explanations  []Explanation `json:"explanations,omitempty"`
	Busy          *Busy         `json:"busy_sources,omitempty"`
}

// Forecast is the forecast command payload; schedule --dry-run emits it too.
type Forecast struct {
	SchemaVersion int           `json:"schema_version"`
	Blocks        []Block       `json:"blocks,omitempty"`
	Shortfalls    []Shortfall   `json:"shortfalls,omitempty"`
	Suggestions   []Suggestion  `json:"suggestions,omitempty"`
	Explanations  []Explanation `json:"explanations,omitempty"`
	Managed       []Managed     `json:"managed_left_out,omitempty"`
	Busy          Busy          `json:"busy_sources"`
}

// Schedule is the schedule command payload.
type Schedule struct {
	SchemaVersion int           `json:"schema_version"`
	Blocks        []Block       `json:"blocks,omitempty"`
	Shortfalls    []Shortfall   `json:"shortfalls,omitempty"`
	Suggestions   []Suggestion  `json:"suggestions,omitempty"`
	Explanations  []Explanation `json:"explanations,omitempty"`
	Writes        []Write       `json:"writes,omitempty"`
	Orphans       []Orphan      `json:"orphaned,omitempty"`
	Reconciled    *Reconcile    `json:"reconciled,omitempty"`
	Dispositions  []Disposition `json:"dispositions,omitempty"`
	Busy          Busy          `json:"busy_sources"`
}

// Refresh is the refresh command payload.
type Refresh struct {
	SchemaVersion int           `json:"schema_version"`
	Dispositions  []Disposition `json:"dispositions,omitempty"`
	Adopted       []Binding     `json:"adopted,omitempty"`
	Captured      []Disposition `json:"captured,omitempty"`
	Busy          Busy          `json:"busy_sources"`
	Conflicts     []Conflict    `json:"conflicts,omitempty"`
}

// Auth is the auth google command payload.
type Auth struct {
	SchemaVersion int    `json:"schema_version"`
	Stored        string `json:"stored"`
}

// ExplainContext is the run context the explanation was produced in.
type ExplainContext struct {
	Command      string    `json:"command"`
	Now          time.Time `json:"now"`
	Timezone     string    `json:"timezone"`
	Granularity  string    `json:"granularity"`
	WindowDays   int       `json:"window_days"`
	Calendar     bool      `json:"calendar"`
	Reconciled   bool      `json:"reconciled"`
	Dispositions int       `json:"effective_dispositions"`
}

// ExplainTask is the explained Task's resolved inputs.
type ExplainTask struct {
	ID           string     `json:"id"`
	Text         string     `json:"text"`
	Graph        string     `json:"graph,omitempty"`
	Marker       string     `json:"marker"`
	Duration     string     `json:"duration"`
	Priority     string     `json:"priority,omitempty"`
	PlanStart    string     `json:"plan_start,omitempty"`
	PlanEnd      string     `json:"plan_end,omitempty"`
	Pin          *time.Time `json:"pin,omitempty"`
	Depends      []string   `json:"depends,omitempty"`
	Order        string     `json:"order,omitempty"`
	Container    bool       `json:"container,omitempty"`
	ParentID     string     `json:"parent_id,omitempty"`
	GraphOrder   int        `json:"graph_order,omitempty"`
	OutlineOrder int        `json:"outline_order"`
}

// Spillover summarizes how many slots landed in primary versus fallback maps.
type Spillover struct {
	PrimaryPlaced  int    `json:"primary_placed"`
	FallbackPlaced int    `json:"fallback_placed"`
	Summary        string `json:"summary"`
}

// Explain is the explain command payload. The eligibility, ordering,
// dependency, window, time-map, placement, block, and outcome fields are the
// planner's actual decision trace; see the package comment.
type Explain struct {
	SchemaVersion int                     `json:"schema_version"`
	Context       ExplainContext          `json:"context"`
	Task          ExplainTask             `json:"task"`
	Eligibility   planner.Eligibility     `json:"eligibility"`
	Ordering      planner.Ordering        `json:"ordering"`
	Properties    []Provenance            `json:"effective_properties,omitempty"`
	Dependency    planner.Dependency      `json:"dependency_gate"`
	Window        planner.Window          `json:"plan_window"`
	TimeMaps      planner.TimeMaps        `json:"time_maps"`
	Placement     *planner.Placement      `json:"placement,omitempty"`
	Blocks        []planner.BlockDecision `json:"final_blocks,omitempty"`
	Outcome       planner.Outcome         `json:"outcome"`
	Dispositions  []Disposition           `json:"dispositions,omitempty"`
	Predecessors  []string                `json:"consideration_order_before,omitempty"`
	Spillover     *Spillover              `json:"spillover,omitempty"`
	Issues        []Issue                 `json:"graph_issues,omitempty"`
	Report        *Shortfall              `json:"report,omitempty"`
	Busy          *Busy                   `json:"busy_sources,omitempty"`
}

// ExplainError is the explain payload when the query matches no Task or more
// than one.
type ExplainError struct {
	SchemaVersion int         `json:"schema_version"`
	Error         string      `json:"error"`
	Query         string      `json:"query"`
	Matches       []TaskMatch `json:"matches"`
}
