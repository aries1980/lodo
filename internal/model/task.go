package model

import (
	"slices"
	"time"
)

// Task is a schedulable unit of work, resolved from a Logseq block. A block is
// a Task iff it is TODO/DOING and carries an explicit duration.
type Task struct {
	ID           string // Logseq block UUID; the Task's identity across runs
	Text         string
	Marker       Marker
	Duration     time.Duration
	TimeMaps     []string // Time Map slugs; empty means the default map
	Priority     Priority
	Deadline     *CivilDate
	PlanStart    *CivilDate
	PlanEnd      *CivilDate
	Pin          *time.Time // from scheduled::
	OutlineOrder int        // depth-first, top-to-bottom Logseq order
	// Depends holds the effective finish-to-start predecessors: explicit
	// depends:: refs plus implicit sequential-chain edges, expanded so a
	// container expands to its leaf Tasks. Sorted, deduplicated; unknown IDs
	// stay so the planner can block and report them.
	Depends []string
	// Order is the sequential/parallel flag for this Task's children
	// (inherited down until overridden; empty means parallel).
	Order OrderMode
	// Container is true when the Task has Task descendants: it is a SkedPal
	// container, never scheduled itself, its duration ignored (the sum of
	// its leaves is shown in check only).
	Container bool
	// ParentID is the direct parent Task's UUID, or empty. Non-Task parents
	// are ignored.
	ParentID string
}

// OrderMode is the sequential/parallel flag for a Task's children.
type OrderMode string

const (
	OrderParallel   OrderMode = "parallel"
	OrderSequential OrderMode = "sequential"
)

// Snapshot is a Task's resolved inputs: the direct and inherited properties
// plus its text. Reconciliation compares a Task's current Snapshot against the
// one recorded with a Stood-down disposition to decide whether the Task's
// inputs have changed.
type Snapshot struct {
	Text      string
	Marker    Marker
	Duration  time.Duration
	TimeMaps  []string
	Priority  Priority
	Deadline  *CivilDate
	PlanStart *CivilDate
	PlanEnd   *CivilDate
	Pin       *time.Time
	Depends   []string
	Order     OrderMode
}

// Snapshot returns t's current resolved inputs.
func (t Task) Snapshot() Snapshot {
	return Snapshot{
		Text:      t.Text,
		Marker:    t.Marker,
		Duration:  t.Duration,
		TimeMaps:  slices.Clone(t.TimeMaps),
		Priority:  t.Priority,
		Deadline:  t.Deadline,
		PlanStart: t.PlanStart,
		PlanEnd:   t.PlanEnd,
		Pin:       t.Pin,
		Depends:   slices.Clone(t.Depends),
		Order:     t.Order,
	}
}
