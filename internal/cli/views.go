package cli

import (
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/planner"
	"github.com/aries1980/lodo/internal/run"
)

// textByTasks indexes Task display text by ID for O(1) rendering lookups.
// All JSON/text converters below build it once instead of scanning tasks
// per row.
func textByTasks(tasks []model.Task) map[string]string {
	return run.TextMap(tasks)
}

// labelFor prefers mapped text, falling back to the id.
func labelFor(byText map[string]string, id string) string {
	return run.LabelWithMap(byText, id)
}

// blockerLabels maps dependency IDs to display text.
func blockerLabels(byText map[string]string, ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, labelFor(byText, id))
	}
	return out
}

// graphByTasks indexes Task graph by ID.
func graphByTasks(tasks []model.Task) map[string]string {
	return run.GraphMap(tasks)
}

// blockViews converts planner blocks with resolved task text.
func blockViews(tasks []model.Task, blocks []model.Block) []blockJSON {
	byText := textByTasks(tasks)
	byGraph := graphByTasks(tasks)
	out := make([]blockJSON, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, blockJSON{
			TaskID: b.TaskID, Task: byText[b.TaskID], Graph: byGraph[b.TaskID],
			Index: b.Index, Start: b.Start, End: b.End, Slots: b.Slots, Kind: string(b.Kind),
		})
	}
	return out
}

// shortfallViews converts planner task reports with resolved task text.
func shortfallViews(tasks []model.Task, reports []planner.TaskReport) []shortfallJSON {
	byText := textByTasks(tasks)
	byGraph := graphByTasks(tasks)
	out := make([]shortfallJSON, 0, len(reports))
	for _, r := range reports {
		out = append(out, shortfallJSON{
			TaskID: r.TaskID, Task: byText[r.TaskID], Graph: byGraph[r.TaskID],
			Required: r.Required, Placed: r.Placed, StoodDown: r.StoodDown,
			Shortfall: r.Shortfall, Reason: string(r.Reason),
			BlockedBy: append([]string(nil), r.BlockedBy...),
		})
	}
	return out
}

// explanationViews converts planner explanations.
func explanationViews(expls []planner.Explanation) []explanationJSON {
	out := make([]explanationJSON, 0, len(expls))
	for _, e := range expls {
		out = append(out, explanationJSON{
			TaskID: e.TaskID, BlockIndex: e.BlockIndex, Kind: string(e.Kind),
			Start: e.Start, Slots: e.Slots, RunStart: e.RunStart, RunSlots: e.RunSlots,
		})
	}
	return out
}

// dispositionViews converts dispositions with resolved task text.
func dispositionViews(tasks []model.Task, disps []model.Disposition) []dispositionJSON {
	byText := textByTasks(tasks)
	byGraph := graphByTasks(tasks)
	out := make([]dispositionJSON, 0, len(disps))
	for _, d := range disps {
		out = append(out, dispositionJSON{
			TaskID: d.TaskID, Task: byText[d.TaskID], Graph: byGraph[d.TaskID], BlockIndex: d.BlockIndex,
			Kind: string(d.Kind), Start: d.Start, End: d.End, Slots: d.Slots,
		})
	}
	return out
}

// orphanViews converts schedule orphans with resolved task text.
func orphanViews(tasks []model.Task, orphans []run.Orphan) []orphanJSON {
	byText := textByTasks(tasks)
	byGraph := graphByTasks(tasks)
	out := make([]orphanJSON, 0, len(orphans))
	for _, o := range orphans {
		out = append(out, orphanJSON{
			EventID: o.EventID, TaskID: o.TaskID, Graph: byGraph[o.TaskID],
			Task:  byText[o.TaskID],
			Block: o.Block, Summary: o.Summary,
		})
	}
	return out
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
