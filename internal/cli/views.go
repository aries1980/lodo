package cli

import (
	"cmp"

	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/planner"
	"github.com/aries1980/lodo/internal/run"
)

type taskLabels struct {
	text  map[string]string
	graph map[string]string
}

// mapViews preserves order and keeps empty views as JSON arrays rather than null.
func mapViews[T, V any](items []T, view func(T) V) []V {
	out := make([]V, len(items))
	for i, item := range items {
		out[i] = view(item)
	}
	return out
}

func mapTaskViews[T, V any](tasks []model.Task, items []T, view func(T, taskLabels) V) []V {
	labels := taskLabels{text: textByTasks(tasks), graph: graphByTasks(tasks)}
	return mapViews(items, func(item T) V { return view(item, labels) })
}

func compareConsiderationOrder(a, b planner.TaskDecision) int {
	ao, bo := a.Ordering.Position, b.Ordering.Position
	if ao == 0 && bo != 0 {
		return 1
	}
	if bo == 0 && ao != 0 {
		return -1
	}
	return cmp.Or(cmp.Compare(ao, bo), cmp.Compare(a.TaskID, b.TaskID))
}

// textByTasks indexes Task display text by ID for O(1) rendering lookups.
// All JSON/text converters below build it once instead of scanning tasks
// per row.
func textByTasks(tasks []model.Task) map[string]string {
	return model.TextByTaskID(tasks)
}

// labelFor prefers mapped text, falling back to the id.
func labelFor(byText map[string]string, id string) string {
	if text, ok := byText[id]; ok && text != "" {
		return text
	}
	return id
}

// blockerLabels maps dependency IDs to display text.
func blockerLabels(byText map[string]string, ids []string) []string {
	return mapViews(ids, func(id string) string { return labelFor(byText, id) })
}

// graphByTasks indexes Task graph slug by ID. The first Task wins when two
// share an id (a duplicate UUID is refused before a real run plans).
func graphByTasks(tasks []model.Task) map[string]string {
	return model.GraphByTaskID(tasks)
}

// blockViews converts planner blocks with resolved task text.
func blockViews(tasks []model.Task, blocks []model.Block) []blockJSON {
	return mapTaskViews(tasks, blocks, func(b model.Block, labels taskLabels) blockJSON {
		return blockJSON{
			TaskID: b.TaskID, Task: labels.text[b.TaskID], Graph: labels.graph[b.TaskID],
			Index: b.Index, Start: b.Start, End: b.End, Slots: b.Slots, Kind: string(b.Kind),
		}
	})
}

// shortfallViews converts planner task reports with resolved task text.
func shortfallViews(tasks []model.Task, reports []planner.TaskReport) []shortfallJSON {
	return mapTaskViews(tasks, reports, func(r planner.TaskReport, labels taskLabels) shortfallJSON {
		return shortfallJSON{
			TaskID: r.TaskID, Task: labels.text[r.TaskID], Graph: labels.graph[r.TaskID],
			Required: r.Required, Placed: r.Placed, StoodDown: r.StoodDown,
			Shortfall: r.Shortfall, Reason: string(r.Reason),
			BlockedBy: append([]string(nil), r.BlockedBy...),
		}
	})
}

// suggestionViews converts planner suggestions with resolved task text.
func suggestionViews(tasks []model.Task, suggestions []planner.Suggestion) []suggestionJSON {
	return mapTaskViews(tasks, suggestions, func(s planner.Suggestion, labels taskLabels) suggestionJSON {
		return suggestionJSON{
			TaskID: s.TaskID, Task: labels.text[s.TaskID], Graph: labels.graph[s.TaskID],
			Reason: s.Reason, Change: s.Change, Impact: s.Impact, Tier: s.Tier,
		}
	})
}

// explanationViews converts planner explanations.
func explanationViews(expls []planner.Explanation) []explanationJSON {
	return mapViews(expls, func(e planner.Explanation) explanationJSON {
		return explanationJSON{
			TaskID: e.TaskID, BlockIndex: e.BlockIndex, Kind: string(e.Kind),
			Start: e.Start, Slots: e.Slots, RunStart: e.RunStart, RunSlots: e.RunSlots,
		}
	})
}

// dispositionViews converts dispositions with resolved task text.
func dispositionViews(tasks []model.Task, disps []model.Disposition) []dispositionJSON {
	return mapTaskViews(tasks, disps, func(d model.Disposition, labels taskLabels) dispositionJSON {
		return dispositionJSON{
			TaskID: d.TaskID, Task: labels.text[d.TaskID], Graph: labels.graph[d.TaskID], BlockIndex: d.BlockIndex,
			Kind: string(d.Kind), Start: d.Start, End: d.End, Slots: d.Slots,
		}
	})
}

// orphanViews converts schedule orphans with resolved task text.
func orphanViews(tasks []model.Task, orphans []run.Orphan) []orphanJSON {
	return mapTaskViews(tasks, orphans, func(o run.Orphan, labels taskLabels) orphanJSON {
		return orphanJSON{
			EventID: o.EventID, TaskID: o.TaskID, Graph: labels.graph[o.TaskID],
			Task:  labels.text[o.TaskID],
			Block: o.Block, Summary: o.Summary,
		}
	})
}

// orphanLabel renders one orphan's task reference for text output.
func orphanLabel(byText map[string]string, o run.Orphan) string {
	if text, ok := byText[o.TaskID]; ok && text != "" {
		return text
	}
	if o.TaskID == "" {
		return "(unreadable pointer)"
	}
	return o.TaskID
}
