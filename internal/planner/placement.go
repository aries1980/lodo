package planner

import (
	"sort"

	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/model"
)

// placementRun is a free run a Block may be drawn from; the day is the local
// day of the run's first slot, which the comparator keys on.
type placementRun struct {
	run grid.Run
	day model.CivilDate
}

// alloc is one Block of a candidate placement: a prefix of run, slots long.
type alloc struct {
	run   int
	slots int
}

// seq is a candidate placement: its Blocks in start order.
type seq []alloc

// choosePlacement picks the arrangement of at most required slots that the
// ADR 0010 comparator rates highest. Every run must be long enough to host a
// Block of floor slots; Blocks are at most one per run, starting at the run's
// first slot.
func choosePlacement(runs []placementRun, required, floor int) []alloc {
	if required < floor || len(runs) == 0 {
		return nil
	}

	placed := maxPlaceable(runs, required, floor)
	if placed == 0 {
		return nil
	}

	best := bestSuffixes(runs, placed, floor)

	// Completeness is fixed to placed; the first Block's day is the next key,
	// so only the earliest feasible day is compared, and only within it.
	var winner seq
	var firstDay model.CivilDate
	for i := 0; i < len(runs) && (winner == nil || runs[i].day == firstDay); i++ {
		for b := floor; b <= min(runs[i].run.Slots, placed); b++ {
			rest := best[i+1][placed-b]
			if rest == nil {
				continue
			}
			if winner == nil {
				firstDay = runs[i].day
			}
			if candidate := append(seq{{run: i, slots: b}}, rest...); better(candidate, winner) {
				winner = candidate
			}
		}
	}
	return winner
}

// maxPlaceable returns the most slots that can legally be placed, which is how
// completeness — the comparators' first key — is fixed before the rest.
func maxPlaceable(runs []placementRun, required, floor int) int {
	lens := make([]int, len(runs))
	for i := range runs {
		lens[i] = runs[i].run.Slots
	}
	sort.Sort(sort.Reverse(sort.IntSlice(lens)))

	sum := 0
	for i := 0; i < min(required/floor, len(lens)); i++ {
		sum += lens[i]
	}
	return min(required, sum)
}

// bestSuffixes returns best[i][m]: the arrangement of exactly m slots drawn
// from runs[i:] that the comparator rates highest, or nil when m slots cannot
// be placed. Fewer Blocks win first, then earlier starts, then larger sizes.
func bestSuffixes(runs []placementRun, placed, floor int) [][]seq {
	best := make([][]seq, len(runs)+1)
	for i := range best {
		best[i] = make([]seq, placed+1)
	}
	best[len(runs)][0] = seq{}

	for i := len(runs) - 1; i >= 0; i-- {
		best[i][0] = seq{}
		limit := runs[i].run.Slots
		for m := 1; m <= placed; m++ {
			candidate := best[i+1][m] // skip this run
			for b := floor; b <= min(limit, m); b++ {
				rest := best[i+1][m-b]
				if rest == nil {
					continue
				}
				if use := append(seq{{run: i, slots: b}}, rest...); better(use, candidate) {
					candidate = use
				}
			}
			best[i][m] = candidate
		}
	}
	return best
}

// better reports whether a rates above b under ADR 0010: fewer Blocks, then
// earlier Block starts, then larger Block sizes.
func better(a, b seq) bool {
	if b == nil {
		return a != nil
	}
	if a == nil {
		return false
	}
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	for i := range a {
		if a[i].run != b[i].run {
			return a[i].run < b[i].run
		}
	}
	for i := range a {
		if a[i].slots != b[i].slots {
			return a[i].slots > b[i].slots
		}
	}
	return false
}
