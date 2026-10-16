// Package reconcile folds the observed calendar into state (calendar.md's
// table): moved Managed events become Pinned Blocks, deleted ones stand
// their Block down, and lodo=true events with no local row are adopted.
//
// Removing dispositions is never this package's job: dispositions persist
// until the Task's inputs change, which the caller detects via snapshot
// mismatch before planning (the planner only sees effective dispositions).
package reconcile

import (
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/norm"
	"github.com/aries1980/lodo/internal/store"
)

// EventID returns the deterministic Managed-Event id for one Block
// (ADR 0007 / calendar.md): "lodo" plus base32hex of sha256(taskUUID + ":"
// + blockIndex), lowercased. Re-inserts are idempotent and a no-op run
// never issues a write. The charset is [a-z0-9], inside Google's
// [a-z0-9-], and 4+26 chars sit well under the id limit.
func EventID(taskID string, blockIndex int) string {
	sum := sha256.Sum256([]byte(taskID + ":" + strconv.Itoa(blockIndex)))
	enc := base32.HexEncoding.WithPadding(base32.NoPadding)
	raw := enc.EncodeToString(sum[:])
	if len(raw) > 26 {
		raw = raw[:26]
	}
	return "lodo" + strings.ToLower(raw)
}

// TaskRef reads the ownership pointer off a Managed event: task uuid and
// block index from its private properties. ok is false when either is
// missing or malformed; the caller leaves such events for check to list as
// orphaned rather than adopting them.
func TaskRef(e calendar.Event) (taskID string, blockIndex int, ok bool) {
	priv := e.ExtendedProperties.Private
	taskID = strings.TrimSpace(priv["task"])
	blockS := strings.TrimSpace(priv["block"])
	if taskID == "" || blockS == "" {
		return "", 0, false
	}
	n, err := strconv.Atoi(blockS)
	if err != nil || n < 0 {
		return "", 0, false
	}
	return taskID, n, true
}

// Result is what the caller commits: binding upserts (moves and adoptions),
// binding deletes (user deletes), and disposition upserts (pinned and
// stood-down). Dispositions are upserts only; nothing is ever removed here.
type Result struct {
	BindingUpserts     []store.Binding
	BindingDeletes     []store.BindingKey
	DispositionUpserts []store.TimedDisposition
}

// Reconcile folds observed Managed events (within the window) and current
// store rows into the next store rows. snapshots is the task-id → last
// resolved snapshot map from the store (written by the last schedule
// commit; refresh cannot resolve the graph, so that is the freshest
// knowledge a delete disposition can carry). granularity derives adopted
// sizes; loc interprets observed spans; now stamps recorded_at.
//
// Rules (calendar.md, made precise):
//   - Moved (bound event id present, times differ): upsert the binding to
//     the observed span and record/refresh a pinned disposition at the
//     observed span (used exactly, never snapped).
//   - Deleted (bound event id absent): delete the binding and record a
//     stood-down with Slots = the binding's slots and Snapshot = the
//     stored snapshot, or an empty snapshot when the task was
//     adopted-but-never-resolved. An empty snapshot matches nothing, so
//     the Block is re-created once and a later user delete stands down
//     properly; inventing a snapshot refresh-time would fabricate intent.
//   - Adopted (lodo=true with no store row): upsert a binding row from the
//     observed span (no snapshot, no disposition).
//   - Time unchanged: nothing.
func Reconcile(observed []calendar.Event, bindings []store.Binding, snapshots map[string]model.Snapshot, granularity time.Duration, loc *time.Location, now time.Time) (Result, error) {
	if loc == nil {
		return Result{}, fmt.Errorf("reconcile: location is required")
	}
	if granularity <= 0 {
		return Result{}, fmt.Errorf("reconcile: granularity must be positive")
	}
	byEvent := make(map[string]store.Binding, len(bindings))
	for _, b := range bindings {
		byEvent[b.EventID] = b
	}
	seen := make(map[string]bool, len(observed))
	var out Result
	for _, e := range observed {
		if !e.Managed() {
			continue
		}
		seen[e.ID] = true
		taskID, blockIndex, ok := TaskRef(e)
		if !ok {
			continue // orphaned: no usable pointer, left for check
		}
		start, end, err := e.Span(loc)
		if err != nil {
			return Result{}, err
		}
		slots := norm.Units(end.Sub(start), granularity)
		if b, bound := byEvent[e.ID]; bound {
			if b.Start.Equal(start) && b.End.Equal(end) {
				continue // time unchanged: nothing
			}
			// Moved: the binding follows the observed span and the Block
			// becomes pinned there. A Task that later gains a scheduled::
			// Pin still records here; the planner resolves precedence
			// (newTaskPlan clears dispositions under a Pin).
			out.BindingUpserts = append(out.BindingUpserts, store.Binding{
				TaskID: taskID, BlockIndex: blockIndex, EventID: e.ID,
				Start: start, End: end, Slots: slots,
			})
			// Snapshot for a pinned disposition is the stored one when
			// known; the planner keeps pinned spans regardless of inputs
			// (they survive unrelated changes until deleted), so it is
			// informational.
			snap := snapshots[taskID]
			out.DispositionUpserts = append(out.DispositionUpserts, store.TimedDisposition{
				Disposition: model.Disposition{
					TaskID: taskID, BlockIndex: blockIndex,
					Kind:  model.DispositionPinned,
					Start: start, End: end,
					Slots: slots, Snapshot: snap,
				},
				RecordedAt: now,
			})
			continue
		}
		// Adopted: Lodo's state was lost (including the
		// calendar-written/store-crashed gap, which is why schedule
		// writes the calendar before the store). No snapshot: refresh
		// cannot resolve the graph.
		out.BindingUpserts = append(out.BindingUpserts, store.Binding{
			TaskID: taskID, BlockIndex: blockIndex, EventID: e.ID,
			Start: start, End: end, Slots: slots,
		})
		_ = blockIndex // bound above; kept for clarity
	}
	for _, b := range bindings {
		if seen[b.EventID] {
			continue
		}
		// Deleted: the binding goes away and the Block stands down with
		// the binding's size and the task's most recent resolved inputs.
		snap, ok := snapshots[b.TaskID]
		if !ok {
			snap = model.Snapshot{} // adopted-but-never-resolved: matches nothing
		}
		out.BindingDeletes = append(out.BindingDeletes, store.BindingKey{TaskID: b.TaskID, BlockIndex: b.BlockIndex})
		out.DispositionUpserts = append(out.DispositionUpserts, store.TimedDisposition{
			Disposition: model.Disposition{
				TaskID: b.TaskID, BlockIndex: b.BlockIndex,
				Kind:  model.DispositionStoodDown,
				Slots: b.Slots, Snapshot: snap,
			},
			RecordedAt: now,
		})
	}
	return out, nil
}

// Effective returns the dispositions that still apply for these tasks.
// Pinned dispositions always apply (they survive unrelated input changes
// until their event is deleted, adopting the Task's current title).
// Stood-down dispositions apply only while the Task's current Snapshot
// still equals the recorded one; a duration (or any input) change re-places
// the full n. Tasks unknown to the graph drop their dispositions here;
// check still lists the stored rows.
func Effective(tasks []model.Task, dispositions []model.Disposition) []model.Disposition {
	byTask := make(map[string]model.Task, len(tasks))
	for _, t := range tasks {
		byTask[t.ID] = t
	}
	var out []model.Disposition
	for _, d := range dispositions {
		t, ok := byTask[d.TaskID]
		if !ok {
			continue
		}
		if d.Kind == model.DispositionPinned {
			out = append(out, d)
			continue
		}
		if d.Kind != model.DispositionStoodDown {
			continue
		}
		if store.SnapshotEqual(t.Snapshot(), d.Snapshot) {
			out = append(out, d)
		}
	}
	return out
}
