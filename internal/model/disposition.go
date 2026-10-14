package model

import "time"

// DispositionKind tells what the user did to a Managed Event.
type DispositionKind string

const (
	// DispositionPinned: the user moved the event; it becomes a Pinned Block
	// at the captured time.
	DispositionPinned DispositionKind = "pinned"

	// DispositionStoodDown: the user deleted the event; that Block is not
	// recreated while the Task's Snapshot still matches.
	DispositionStoodDown DispositionKind = "stood-down"
)

// Disposition is a recorded user response to a Managed Event of one Block.
// Only effective dispositions — those the reconciliation has decided apply —
// are consumed by the planner.
type Disposition struct {
	TaskID     string
	BlockIndex int
	Kind       DispositionKind

	// Pinned: the span the user moved the event to, used exactly (never snapped).
	Start time.Time
	End   time.Time

	// StoodDown: the removed Block's size in granularity units, and the Task's
	// inputs when the disposition was recorded.
	Slots    int
	Snapshot Snapshot
}
