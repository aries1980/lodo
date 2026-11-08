package model

// TextByTaskID indexes Task display text by ID for O(1) lookups. When IDs
// repeat, the last Task wins; callers that require unique IDs validate them
// before planning.
func TextByTaskID(tasks []Task) map[string]string {
	out := make(map[string]string, len(tasks))
	for _, t := range tasks {
		out[t.ID] = t.Text
	}
	return out
}

// GraphByTaskID indexes Task graph slugs by ID. The first Task wins when
// IDs repeat; callers that require unique IDs validate them before planning.
func GraphByTaskID(tasks []Task) map[string]string {
	out := make(map[string]string, len(tasks))
	for _, t := range tasks {
		if _, ok := out[t.ID]; !ok {
			out[t.ID] = t.Graph
		}
	}
	return out
}
