package model

// Marker is a Logseq task marker.
type Marker string

const (
	MarkerTODO      Marker = "TODO"
	MarkerDOING     Marker = "DOING"
	MarkerDONE      Marker = "DONE"
	MarkerCANCELLED Marker = "CANCELLED"
)

// Priority is a Logseq priority. The zero value means "absent".
type Priority string

const (
	PriorityA Priority = "A"
	PriorityB Priority = "B"
	PriorityC Priority = "C"
)

// Rank orders Priority values: A < B < C < absent. Unknown values sort with
// the absent ones, so malformed input can never outrank a real priority.
func (p Priority) Rank() int {
	switch p {
	case PriorityA:
		return 0
	case PriorityB:
		return 1
	case PriorityC:
		return 2
	default:
		return 3
	}
}
