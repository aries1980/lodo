// Calendar writes: diffPlan's WriteOps applied in delete/patch/insert phases
// with cross-healing fallbacks for List-then-write races. Calendar first,
// store second (ADR 0007).
package run

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/calendar"
	"github.com/aries1980/lodo/internal/fanout"
	"github.com/aries1980/lodo/internal/reconcile"
)

// writeWorkers bounds concurrent calendar writes within one operation phase.
// Four keeps Google's write quota comfortable and matches the read bound
// (ADR 0025).
const writeWorkers = 4

// applyWrites issues the calendar mutations in stable phases: deletes,
// patches, then inserts. Each phase runs independently with a bounded
// fan-out; later phases wait for earlier ones. The barrier matters: a
// corrupt Managed event with an unusable pointer is deleted, while the plan
// can insert a Block whose deterministic id is the same event id. Delete
// must finish before that insert, or the insert's 409 fallback patches the
// event the delete then removes.
//
// A delete 404 is already-gone success; anything else is operational.
// Inserts and patches cross-heal: an insert that hits an existing id (the
// event lives on the calendar but outside the list window, e.g. in the
// past) falls back to patch, and a patch that hits a missing event falls
// back to insert. Either fallback covers the List-then-write race with an
// external edit.
func applyWrites(ctx context.Context, cal Calendar, writes []WriteOp) error {
	for _, op := range []string{"delete", "patch", "insert"} {
		batch := writesOfOp(writes, op)
		if len(batch) == 0 {
			continue
		}
		_, err := fanout.Do(ctx, len(batch), writeWorkers, func(ctx context.Context, i int) (struct{}, error) {
			return struct{}{}, applyWrite(ctx, cal, batch[i])
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// writesOfOp filters one phase, preserving the stable event-id order
// diffPlan produced.
func writesOfOp(writes []WriteOp, op string) []WriteOp {
	out := make([]WriteOp, 0, len(writes))
	for _, write := range writes {
		if write.Op == op {
			out = append(out, write)
		}
	}
	return out
}

// applyWrite dispatches one calendar mutation to its per-op helper so each
// path stays flat.
func applyWrite(ctx context.Context, cal Calendar, op WriteOp) error {
	switch op.Op {
	case "delete":
		return applyDelete(ctx, cal, op)
	case "patch":
		return applyPatch(ctx, cal, op)
	case "insert":
		return applyInsert(ctx, cal, op)
	default:
		return fmt.Errorf("run: unknown write op %q", op.Op)
	}
}

// applyDelete removes one Managed event. A missing event is already-gone
// success.
func applyDelete(ctx context.Context, cal Calendar, op WriteOp) error {
	err := cal.Delete(ctx, op.EventID)
	if err == nil || isNotFound(err) {
		return nil
	}
	return err
}

// applyInsert creates one Managed event. An existing id means the event
// lives on the calendar outside the list window, so patch it into place.
func applyInsert(ctx context.Context, cal Calendar, op WriteOp) error {
	if _, err := cal.Insert(ctx, insertPayload(op)); err == nil {
		return nil
	} else if !isAlreadyExists(err) {
		return err
	}
	return patchExisting(ctx, cal, op)
}

// patchExisting is the insert fallback: move the already-present event to
// the planned span. A miss means it was deleted between the failed insert
// and this patch, so one insert retry converges.
func patchExisting(ctx context.Context, cal Calendar, op WriteOp) error {
	if _, err := cal.Patch(ctx, op.EventID, patchPayload(op)); err == nil {
		return nil
	} else if !isNotFound(err) {
		return err
	}
	_, err := cal.Insert(ctx, insertPayload(op))
	return err
}

// applyPatch moves one Managed event to its planned span. A missing event
// means it vanished after List, so create it.
func applyPatch(ctx context.Context, cal Calendar, op WriteOp) error {
	if _, err := cal.Patch(ctx, op.EventID, patchPayload(op)); err == nil {
		return nil
	} else if !isNotFound(err) {
		return err
	}
	return insertMissing(ctx, cal, op)
}

// insertMissing is the patch fallback: recreate the vanished event. A
// collision means it was recreated between the failed patch and this
// insert, so one patch retry converges.
func insertMissing(ctx context.Context, cal Calendar, op WriteOp) error {
	if _, err := cal.Insert(ctx, insertPayload(op)); err == nil {
		return nil
	} else if !isAlreadyExists(err) {
		return err
	}
	_, err := cal.Patch(ctx, op.EventID, patchPayload(op))
	return err
}

func insertPayload(op WriteOp) calendar.Event {
	event := eventTimes(op)
	event.ID = op.EventID
	event.ExtendedProperties = calendar.Extended{Private: privateProps(op)}
	event.Reminders = &calendar.Reminders{UseDefault: true}
	return event
}

func patchPayload(op WriteOp) calendar.Event {
	event := eventTimes(op)
	event.ExtendedProperties = calendar.Extended{Private: privateProps(op)}
	return event
}

// privateProps is the Managed-event ownership record plus the span marker
// reconciliation uses to recognise Lodo's own delayed write. Patches send
// the whole map: Google's PATCH replaces the private bag, so omitting the
// ownership keys would orphan the event.
func privateProps(op WriteOp) map[string]string {
	priv := map[string]string{
		"lodo": "true", "task": op.TaskID, "block": strconv.Itoa(op.BlockIndex),
		"lodo-span": reconcile.SpanMarker(op.Start, op.End),
	}
	if op.Graph != "" {
		priv["graph"] = op.Graph
	}
	return priv
}

func eventTimes(op WriteOp) calendar.Event {
	tz := ""
	if !op.Start.IsZero() {
		if loc := op.Start.Location(); loc != nil {
			tz = loc.String()
		}
	}
	return calendar.Event{
		Summary: op.Summary,
		Start:   calendar.EventTime{DateTime: op.Start.Format(time.RFC3339), TimeZone: tz},
		End:     calendar.EventTime{DateTime: op.End.Format(time.RFC3339), TimeZone: tz},
	}
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	if code, ok := calendar.StatusCodeOf(err); ok {
		// 410 is Google's "gone" for an event that was deleted.
		if code == http.StatusNotFound || code == http.StatusGone {
			return true
		}
	}
	s := err.Error()
	return strings.Contains(s, "404") || strings.Contains(s, "410 Gone") ||
		strings.Contains(s, "notFound") || strings.Contains(s, "no such event")
}

// isAlreadyExists reports Google's duplicate-id failure on insert: HTTP 409
// ("The requested identifier already exists"). The deterministic event id
// collides whenever the event lives on the calendar but outside the list
// window, so the caller patches the existing event into place instead.
func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	if code, ok := calendar.StatusCodeOf(err); ok && code == http.StatusConflict {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "already exists") || strings.Contains(s, "duplicate")
}
