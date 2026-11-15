package cli

import (
	"cmp"

	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/planner"
	"github.com/aries1980/lodo/internal/run"
)

// This file holds the label lookups and ordering helpers shared by the text
// renderers. The JSON mappings and their payload types live in wire.go and
// internal/wire.

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
