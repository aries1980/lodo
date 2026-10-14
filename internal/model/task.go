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
}

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
	}
}
