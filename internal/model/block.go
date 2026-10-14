package model

import "time"

// BlockKind tells how a Block came to be.
type BlockKind string

const (
	BlockPin      BlockKind = "pin"      // a scheduled:: the user authored
	BlockPinned   BlockKind = "pinned"   // a managed event the user moved by hand
	BlockFlexible BlockKind = "flexible" // placed by the planner
)

// Block is one contiguous interval of scheduled time belonging to a Task.
type Block struct {
	TaskID string
	Index  int // stable per-Task block index; pinned and stood-down Blocks reserve theirs
	Start  time.Time
	End    time.Time
	Slots  int // whole granularity units of work
	Kind   BlockKind
}
