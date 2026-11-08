package planner

import (
	"time"

	"github.com/aries1980/lodo/internal/model"
)

// TaskDecision records why one Task was (or was not) placed, reflecting the
// actual planner logic. Explain and --json consume it; it must not affect
// placement (ADR 0006: Plan stays pure/deterministic).
type TaskDecision struct {
	TaskID string `json:"task_id"`
	Text   string `json:"task,omitempty"`
	Graph  string `json:"graph,omitempty"`

	Eligibility Eligibility `json:"eligibility"`
	Ordering    Ordering    `json:"ordering,omitempty"`
	Dependency  Dependency  `json:"dependency,omitempty"`
	Window      Window      `json:"window,omitempty"`
	TimeMaps    TimeMaps    `json:"time_maps,omitempty"`
	Placement   *Placement  `json:"placement,omitempty"`

	// Final blocks and outcome.
	Blocks  []BlockDecision `json:"blocks,omitempty"`
	Outcome Outcome         `json:"outcome"`
	// Dispositions are the effective pinned/stood-down rows the planner saw
	// for this task (user moves/deletes folded back via reconciliation).
	Dispositions []model.Disposition `json:"dispositions,omitempty"`
}

// Eligibility explains why the Task entered (or did not enter) flexible placement.
type Eligibility struct {
	Container      bool   `json:"container,omitempty"`
	Pin            bool   `json:"pin,omitempty"`
	RequiredSlots  int    `json:"required_slots"`
	StoodDownSlots int    `json:"stood_down_slots,omitempty"`
	PinnedSlots    int    `json:"pinned_slots,omitempty"`
	FlexibleSlots  int    `json:"flexible_slots"`
	BeyondHorizon  bool   `json:"beyond_horizon,omitempty"`
	Considered     bool   `json:"considered"`
	SkipReason     string `json:"skip_reason,omitempty"`
}

// Ordering records task-order context and tie-breaks that affected consideration.
type Ordering struct {
	Kind         string `json:"kind,omitempty"` // pin, time-bound, flexible, blocked, cycle-blocked, container, satisfied
	GraphOrder   int    `json:"graph_order,omitempty"`
	Priority     string `json:"priority,omitempty"`
	PriorityRank int    `json:"priority_rank,omitempty"`
	OutlineOrder int    `json:"outline_order"`
	PlanEnd      string `json:"plan_end,omitempty"`
	// Position is the 1-based index in consideration order; 0 means the task
	// never became ready (blocked, cycle, beyond horizon, container, pin).
	// Total is the number of tasks in consideration order this run.
	Position int `json:"position,omitempty"`
	Total    int `json:"total,omitempty"`
	// TieBreak names the key that decided this Task's position among unrelated
	// peers when relevant (time-bound-first, plan-end, graph-order, priority,
	// outline-order, graph, id, dependency, only-ready, blocked, ...).
	TieBreak       string `json:"tie_break,omitempty"`
	TieBreakDetail string `json:"tie_break_detail,omitempty"`
}

// Dependency records the finish-to-start gate.
type Dependency struct {
	Depends      []string  `json:"depends,omitempty"`
	Blocked      bool      `json:"blocked,omitempty"`
	BlockReason  string    `json:"block_reason,omitempty"`
	BlockedBy    []string  `json:"blocked_by,omitempty"`
	LowerBound   time.Time `json:"lower_bound,omitempty"`
	CycleMember  bool      `json:"cycle_member,omitempty"`
	PinViolation bool      `json:"pin_violation,omitempty"`
}

// Window records the effective plan/horizon window and overdue handling.
type Window struct {
	Overdue    bool      `json:"overdue,omitempty"`
	PlanStart  string    `json:"plan_start,omitempty"`
	PlanEnd    string    `json:"plan_end,omitempty"`
	From       time.Time `json:"from"`
	To         time.Time `json:"to"`
	Bound      time.Time `json:"bound,omitempty"`
	HorizonEnd string    `json:"horizon_end,omitempty"`
}

// TimeMaps records which maps/tiers were used.
type TimeMaps struct {
	Requested     []string   `json:"requested,omitempty"`
	Primary       []string   `json:"primary,omitempty"`
	Fallback      []string   `json:"fallback,omitempty"`
	BestTier      string     `json:"best_tier,omitempty"`
	NoUpwardSpill bool       `json:"no_upward_spill,omitempty"`
	Passes        []TierPass `json:"passes,omitempty"`
}

// TierPass summarizes one tier pass of the actual placement search.
type TierPass struct {
	Tier                string `json:"tier"`
	Primary             bool   `json:"primary"`
	FallbackAllowed     bool   `json:"fallback_allowed"`
	SlotsBefore         int    `json:"slots_before"`
	SlotsPlaced         int    `json:"slots_placed"`
	SlotsAfter          int    `json:"slots_after"`
	Floor               int    `json:"floor"`
	EligibleTierSlots   int    `json:"eligible_tier_slots"`
	WindowSlots         int    `json:"window_slots"`
	FreeSlots           int    `json:"free_slots"`
	RunsConsidered      int    `json:"runs_considered"`
	RunsBelowFloor      int    `json:"runs_below_floor"`
	RejectedBusy        int    `json:"rejected_busy,omitempty"`
	RejectedOutOfWindow int    `json:"rejected_out_of_window,omitempty"`
	RejectedBelowFloor  int    `json:"rejected_below_floor_slots,omitempty"`
	// Breakdown of RejectedBusy into exclusive buckets (a slot blocked by
	// both a pin and external busy counts as pin): external calendar busy,
	// pins, user-moved (pinned) blocks, and blocks placed for earlier tasks
	// in consideration order (displacement pressure).
	RejectedExternal int `json:"rejected_external,omitempty"`
	RejectedPin      int `json:"rejected_pin,omitempty"`
	RejectedPinned   int `json:"rejected_pinned,omitempty"`
	RejectedPlaced   int `json:"rejected_placed_earlier,omitempty"`
	// Breakdown of RejectedOutOfWindow: before the dependency/now bound
	// (past or waiting on dependencies) vs outside the plan window
	// (plan-start/plan-end/horizon clip).
	RejectedBeforeBound int                 `json:"rejected_before_bound,omitempty"`
	RejectedOutsidePlan int                 `json:"rejected_outside_plan,omitempty"`
	Comparator          *ComparatorDecision `json:"comparator,omitempty"`
}

// ComparatorDecision records the ADR 0010/0014 comparator outcome.
type ComparatorDecision struct {
	Completeness int          `json:"completeness"`
	Candidates   int          `json:"candidates"`
	FeasibleRuns int          `json:"feasible_runs"`
	ChosenBlocks int          `json:"chosen_blocks"`
	DecisionKeys []string     `json:"decision_keys,omitempty"`
	Allocations  []Allocation `json:"allocations,omitempty"`
}

// Allocation is one chosen block from a comparator decision.
type Allocation struct {
	RunStart time.Time `json:"run_start"`
	RunSlots int       `json:"run_slots"`
	Slots    int       `json:"slots"`
}

// Placement summarizes the flexible placement search for a Task.
type Placement struct {
	Floor int `json:"floor"`
	// RequiredSlots is the whole duration in granularity units
	// (ceil(duration/granularity)); the rounding source for granularity.
	RequiredSlots int `json:"required_slots,omitempty"`
}

// BlockDecision records one final block for explain output.
type BlockDecision struct {
	Index    int             `json:"index"`
	Kind     model.BlockKind `json:"kind"`
	Start    time.Time       `json:"start"`
	End      time.Time       `json:"end"`
	Slots    int             `json:"slots"`
	RunStart time.Time       `json:"run_start,omitempty"`
	RunSlots int             `json:"run_slots,omitempty"`
}

// Outcome is the final per-task result.
type Outcome struct {
	Required  int      `json:"required"`
	Placed    int      `json:"placed"`
	StoodDown int      `json:"stood_down"`
	Shortfall int      `json:"shortfall"`
	Reason    string   `json:"reason,omitempty"`
	BlockedBy []string `json:"blocked_by,omitempty"`
}
