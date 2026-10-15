package resolve

// Issue kinds are stable strings so check --json output is deterministic
// and greppable. Every listed problem is reported and skipped — never
// guessed at (logseq-integration.md).
//
// The report type is the graph section of check; the state section
// (stood-down, orphaned) lives in the CLI report, so these kinds cover the
// graph only.
const (
	// IssueMissingDuration is a TODO/DOING block with no duration::: not a
	// Task per ADR 0004 (the duration is the gate), listed so the omission
	// is visible.
	IssueMissingDuration = "todo-missing-duration"
	// IssueBadProperty is a property Lodo knows whose value cannot be
	// parsed. The property is dropped and the Task keeps the rest of its
	// inputs.
	IssueBadProperty = "bad-property-value"
	// IssueUnknownProperty is a property key Lodo does not read, left alone
	// but named, because a block meant as a Task may miss the key that
	// mattered.
	IssueUnknownProperty = "unknown-property"
	// IssueUnknownTimeMap is a time-map:: slug no Time Map defines.
	IssueUnknownTimeMap = "unknown-time-map"
	// IssueBadWindow is a Time Map window line that does not parse.
	IssueBadWindow = "bad-window"
	// IssueBadTimeMap is a Time Map whose name has no sluggable text.
	IssueBadTimeMap = "bad-time-map"
	// IssueBadDefault is a default:: that is neither true nor false.
	IssueBadDefault = "bad-default-value"
	// IssueDefaultMapCount is a broken default-map rule: zero defaults, or
	// more than exactly one. With no single default, none applies.
	IssueDefaultMapCount = "default-map-count"
	// IssueDuplicateMap is two top-level blocks on the Time Maps page whose
	// names slugify to the same key.
	IssueDuplicateMap = "duplicate-map-slug"
	// IssueMissingTimeMapsPage is the configured Time Maps page absent from
	// the graph.
	IssueMissingTimeMapsPage = "missing-time-maps-page"
	// IssueMissingBlockID is a task-marked block that has no UUID, so it
	// cannot be identified across runs.
	IssueMissingBlockID = "missing-block-id"
	// IssueUnknownDependency is a depends:: ref to no Task and no
	// DONE/CANCELLED block: a typo or a note, never guessed at. The
	// dependent is blocked until it is fixed.
	IssueUnknownDependency = "unknown-dependency"
	// IssueDependencyCycle is a depends:: / sequential-chain cycle
	// (including self-dependency). Tasks in the cycle are left
	// unscheduled and reported, never force-ordered.
	IssueDependencyCycle = "dependency-cycle"
)

// Issue is one graph problem reported by check. Context locates the block
// ("page 2026-10-15, block 34444444…"), Detail explains what was skipped.
type Issue struct {
	Kind    string `json:"kind"`
	Context string `json:"context"`
	Detail  string `json:"detail"`
}
