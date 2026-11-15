package planner

import (
	"cmp"
	"slices"

	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/set"
)

// preferenceTiers is the order placement fills a Task's hours: best first.
var preferenceTiers = [...]model.Tier{model.TierGreen, model.TierYellow, model.TierRed}

// timeMapSets is the one place that decides which Time Maps and tiers a Task
// may use: its own maps as primary and, when it names any, the default maps at
// its best primary tier or worse as fallback.
type timeMapSets struct {
	primary  []model.TimeMap
	fallback []model.TimeMap
	best     int
}

// newTimeMapSets partitions the Time Maps a Task may use. A Task with no
// explicit time-map:: uses the default maps with no fallback. A Task naming
// its own maps keeps those as primary and spills into the default maps;
// unknown-only names resolve to nothing and stay strict so the
// unknown-time-map issue is not masked. Defaults are sorted by slug and
// repeated names are dropped, so the selection and its trace are deterministic.
func newTimeMapSets(task model.Task, maps map[string]model.TimeMap) timeMapSets {
	defaults := make([]model.TimeMap, 0, len(maps))
	for _, timeMap := range maps {
		if timeMap.Default {
			defaults = append(defaults, timeMap)
		}
	}
	slices.SortFunc(defaults, func(a, b model.TimeMap) int {
		return cmp.Compare(a.Slug, b.Slug)
	})

	if len(task.TimeMaps) == 0 {
		return timeMapSets{primary: defaults, best: bestTierRank(defaults)}
	}

	primarySlugs := set.New[string]()
	primary := make([]model.TimeMap, 0, len(task.TimeMaps))
	for _, slug := range task.TimeMaps {
		timeMap, ok := maps[slug]
		if !ok || !primarySlugs.Add(timeMap.Slug) {
			continue
		}
		primary = append(primary, timeMap)
	}
	if len(primary) == 0 {
		return timeMapSets{}
	}

	var fallback []model.TimeMap
	for _, timeMap := range defaults {
		if !primarySlugs.Has(timeMap.Slug) {
			fallback = append(fallback, timeMap)
		}
	}
	return timeMapSets{primary: primary, fallback: fallback, best: bestTierRank(primary)}
}

// fallbackAllowed reports whether tier may draw on the fallback maps: only at
// the Task's best primary tier or worse, never upward.
func (s timeMapSets) fallbackAllowed(tier model.Tier) bool {
	return len(s.fallback) > 0 && tierRank(tier) >= s.best
}

// primaryMask returns the slots of one preference tier in the primary set.
func (s timeMapSets) primaryMask(g *grid.Grid, tier model.Tier) grid.Mask {
	return tierEligibility(g, s.primary, tier)
}

// fallbackMask returns the slots of one preference tier in the fallback set.
func (s timeMapSets) fallbackMask(g *grid.Grid, tier model.Tier) grid.Mask {
	return tierEligibility(g, s.fallback, tier)
}

// eligibleMask is every slot placement may use: the whole primary set plus
// the fallback set at the best primary tier or worse.
func (s timeMapSets) eligibleMask(g *grid.Grid) grid.Mask {
	m := grid.NewMask(g.Len())
	for _, tier := range preferenceTiers {
		m.OrInto(s.primaryMask(g, tier))
		if s.fallbackAllowed(tier) {
			m.OrInto(s.fallbackMask(g, tier))
		}
	}
	return m
}

// eligibleSlots counts the slots g offers this Task. Callers that want a
// weekly capacity estimate build g over one week. Busy time and the minimum
// Block floor are placement concerns and are not subtracted here.
func (s timeMapSets) eligibleSlots(g *grid.Grid) int {
	return s.eligibleMask(g).Count()
}

// mapsForHours returns the maps weekday advice should consider: every primary
// window, plus fallback windows at the best primary tier or worse.
func (s timeMapSets) mapsForHours() []model.TimeMap {
	if len(s.fallback) == 0 {
		return s.primary
	}
	out := append([]model.TimeMap(nil), s.primary...)
	for _, timeMap := range s.fallback {
		filtered := filterWindowsAtLeast(timeMap, s.best)
		if len(filtered.Windows) > 0 {
			out = append(out, filtered)
		}
	}
	return out
}

// trace records the selection for the explain trace.
func (s timeMapSets) trace(requested []string) TimeMaps {
	return TimeMaps{
		Requested:     append([]string(nil), requested...),
		Primary:       mapSlugs(s.primary),
		Fallback:      mapSlugs(s.fallback),
		BestTier:      string(tierByRank(s.best)),
		NoUpwardSpill: len(requested) > 0 && len(s.primary) > 0 && len(s.fallback) > 0,
	}
}

// tierEligibility returns the slots of one preference tier in the given map
// set. A slot covered by two tiers takes the better one: yellow excludes
// green, red excludes both.
func tierEligibility(g *grid.Grid, selected []model.TimeMap, tier model.Tier) grid.Mask {
	green := tierMask(g, selected, model.TierGreen)
	switch tier {
	case model.TierYellow:
		return tierMask(g, selected, model.TierYellow).AndNot(green)
	case model.TierRed:
		return tierMask(g, selected, model.TierRed).AndNot(green).AndNot(tierMask(g, selected, model.TierYellow))
	default:
		return green
	}
}

// tierMask is the union of windows of one tier. An empty or unknown tier
// word counts as green, matching the parser's default.
func tierMask(g *grid.Grid, maps []model.TimeMap, tier model.Tier) grid.Mask {
	m := grid.NewMask(g.Len())
	for _, timeMap := range maps {
		filtered := model.TimeMap{Windows: windowsOfTier(timeMap.Windows, tier)}
		if len(filtered.Windows) == 0 {
			continue
		}
		m.OrInto(g.TimeMapMask(filtered))
	}
	return m
}

func windowsOfTier(windows []model.Window, tier model.Tier) []model.Window {
	var out []model.Window
	for _, w := range windows {
		if windowTier(w) == tier {
			out = append(out, w)
		}
	}
	return out
}

func windowTier(w model.Window) model.Tier {
	switch w.Tier {
	case model.TierYellow, model.TierRed:
		return w.Tier
	default:
		return model.TierGreen
	}
}

// tierRank orders tiers by preference: green first, then yellow, then red.
func tierRank(t model.Tier) int {
	switch t {
	case model.TierYellow:
		return 1
	case model.TierRed:
		return 2
	default:
		return 0
	}
}

func tierByRank(rank int) model.Tier {
	switch rank {
	case 1:
		return model.TierYellow
	case 2:
		return model.TierRed
	default:
		return model.TierGreen
	}
}

// bestTierRank is the best (lowest-rank) tier in the given maps. A set with
// no windows at all ranks green, so an empty project map tries the default
// map from its green tier instead of shortfalling.
func bestTierRank(maps []model.TimeMap) int {
	best := 2
	found := false
	for _, timeMap := range maps {
		for _, w := range timeMap.Windows {
			if rank := tierRank(windowTier(w)); !found || rank < best {
				best = rank
				found = true
			}
		}
	}
	if !found {
		return 0 // empty set: try the fallback from its best tier
	}
	return best
}

func mapSlugs(maps []model.TimeMap) []string {
	if len(maps) == 0 {
		return nil
	}
	out := make([]string, 0, len(maps))
	for _, timeMap := range maps {
		out = append(out, timeMap.Slug)
	}
	return out
}

func filterWindowsAtLeast(m model.TimeMap, minRank int) model.TimeMap {
	out := model.TimeMap{Slug: m.Slug, Default: m.Default}
	for _, w := range m.Windows {
		if tierRank(windowTier(w)) < minRank {
			continue
		}
		out.Windows = append(out.Windows, w)
	}
	return out
}
