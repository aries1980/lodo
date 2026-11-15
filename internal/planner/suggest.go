package planner

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/set"
)

type Suggestion struct {
	TaskID string
	Reason string
	Change string
	Impact string
	Tier   string
}

const (
	SuggestExtendPlanEnd        = "extend-plan-end"
	SuggestReduceDuration       = "reduce-duration"
	SuggestSpillToDefaultYellow = "spill-to-default-yellow"
	SuggestSpillToDefaultGreen  = "spill-to-default-green"
	SuggestSpillToDefaultRed    = "spill-to-default-red"
	SuggestAddTimeMapHours      = "add-time-map-hours"
	SuggestFixTimeMap           = "fix-time-map"
	SuggestAddPlanEnd           = "add-plan-end"
	SuggestCompleteDependency   = "complete-dependency"
	SuggestFixDependency        = "fix-dependency"
	SuggestFixCycle             = "fix-cycle"
	SuggestRestoreStoodDown     = "restore-stood-down"
)

// SuggestSpillToDefault is the historic name for the yellow spill reason.
// Prefer SuggestSpillToDefaultYellow in new code; the emitted reason string
// is unchanged.
const SuggestSpillToDefault = SuggestSpillToDefaultYellow

const (
	suggestMinExtendDays = 2
	suggestMaxExtendDays = 28
	// suggestExtendShortfallThreshold gates the capacity-based day estimate:
	// small shortfalls use the minimal extension.
	suggestExtendShortfallThreshold = 8
	// thinDayMinutes names the "actually thin" cutoff used when choosing
	// which weekdays to name in add-time-map-hours advice.
	thinDayMinutes = 240
	// thinDaysToName is how many weekdays the advice names.
	thinDaysToName = 2
	// weeklyDays is the span the capacity estimate averages over. It is fixed
	// rather than WindowDays because the advice quotes a weekly rate.
	weeklyDays = 7
)

// suggestInput is the planner input plus the report, cycle set and derived
// lookups that suggestions are computed from.
type suggestInput struct {
	Input
	report     Report
	cycleSet   set.Set[string]
	maps       map[string]model.TimeMap
	capacity   *grid.Grid
	today      model.CivilDate
	horizonEnd model.CivilDate
}

func Suggest(in Input, schedule Schedule, cycleSet set.Set[string]) []Suggestion {
	s := suggestInput{
		Input:    in,
		report:   schedule.Report,
		cycleSet: cycleSet,
		maps:     indexTimeMaps(in.TimeMaps),
	}
	s.today = model.CivilDateOf(s.Now.In(s.Location))
	s.horizonEnd = s.today.AddDays(s.WindowDays)
	// Capacity is a nominal week of authored hours, not free time from Now:
	// busy time and the current day's elapsed hours are placement concerns.
	s.capacity = grid.Build(s.Now, s.Location, s.Granularity, weeklyDays)
	byID := make(map[string]model.Task, len(s.Tasks))
	for _, t := range s.Tasks {
		byID[t.ID] = t
	}
	var out []Suggestion
	for _, r := range s.report.Tasks {
		if r.Shortfall <= 0 {
			continue
		}
		task, ok := byID[r.TaskID]
		if !ok {
			continue
		}
		if task.Pin != nil {
			continue
		}
		if blockedReason(r.Reason) {
			// A dependency/cycle/unknown blocker is the thing to fix;
			// restoring stood-down blocks cannot unblock it, so keep the
			// advice focused on the blocker.
			out = append(out, suggestBlocked(r, task, byID, s.cycleSet)...)
			continue
		}
		if r.StoodDown > 0 {
			out = append(out, Suggestion{
				TaskID: r.TaskID, Reason: SuggestRestoreStoodDown,
				Change: "restore stood-down block(s)", Impact: "reclaims user-deleted time",
			})
		}
		if authoring, done := suggestAuthoring(r, task, s.maps); done {
			out = append(out, authoring...)
			continue
		}
		out = append(out, suggestCapacity(r, task, s)...)
	}
	slices.SortFunc(out, func(a, b Suggestion) int {
		return cmp.Or(cmp.Compare(a.TaskID, b.TaskID), cmp.Compare(a.Reason, b.Reason))
	})
	return out
}

// blockedReason reports whether the shortfall is a dependency-gating issue
// rather than a capacity issue. Such tasks get blocker fixes only.
func blockedReason(r Reason) bool {
	switch r {
	case ReasonBlockedByDependency, ReasonUnknownDependency, ReasonDependencyCycle, ReasonPinPrecedesDependency:
		return true
	default:
		return false
	}
}

func suggestBlocked(r TaskReport, task model.Task, byID map[string]model.Task, cycleSet set.Set[string]) []Suggestion {
	switch r.Reason {
	case ReasonBlockedByDependency:
		if cycleSet != nil && blockedByCycle(r.BlockedBy, cycleSet) {
			return []Suggestion{{
				TaskID: r.TaskID, Reason: SuggestFixCycle,
				Change: "break dependency cycle", Impact: "unblocks tasks in cycle",
			}}
		}
		if len(r.BlockedBy) > 0 {
			first := r.BlockedBy[0]
			label := first
			if bt, ok := byID[first]; ok && bt.Text != "" {
				label = bt.Text
			}
			return []Suggestion{{
				TaskID: r.TaskID, Reason: SuggestCompleteDependency,
				Change: fmt.Sprintf("complete %s first", label),
				Impact: fmt.Sprintf("unblocks %s", displayText(task)),
			}}
		}
		return []Suggestion{{
			TaskID: r.TaskID, Reason: SuggestCompleteDependency,
			Change: "complete dependencies first", Impact: "unblocks dependent",
		}}
	case ReasonUnknownDependency:
		return []Suggestion{{
			TaskID: r.TaskID, Reason: SuggestFixDependency,
			Change: fmt.Sprintf("fix depends:: reference %s", strings.Join(r.BlockedBy, ", ")),
			Impact: "unblocks task",
		}}
	case ReasonDependencyCycle:
		return []Suggestion{{
			TaskID: r.TaskID, Reason: SuggestFixCycle,
			Change: "break dependency cycle", Impact: "unblocks tasks in cycle",
		}}
	default:
		return nil
	}
}

// suggestAuthoring handles map/plan authoring issues that mask capacity.
// It reports done=true when capacity relief must not also be suggested.
func suggestAuthoring(r TaskReport, task model.Task, maps map[string]model.TimeMap) ([]Suggestion, bool) {
	if unknowns := unknownMapSlugs(task, maps); len(unknowns) > 0 {
		return []Suggestion{{
			TaskID: r.TaskID, Reason: SuggestFixTimeMap,
			Change: fmt.Sprintf("fix time-map:: %s (unknown)", strings.Join(unknowns, ", ")),
			Impact: "enables placement",
		}}, true
	}
	return nil, false
}

// suggestCapacity emits the capacity-relief advice for a task whose blockers
// and authoring are already resolved: optional spill, optional plan-end
// extension, plus duration and Time Map hour relief.
func suggestCapacity(r TaskReport, task model.Task, in suggestInput) []Suggestion {
	sets := newTimeMapSets(task, in.maps)
	var out []Suggestion
	if task.Deadline != nil && task.PlanStart == nil && task.PlanEnd == nil {
		out = append(out, Suggestion{
			TaskID: r.TaskID, Reason: SuggestAddPlanEnd,
			Change: fmt.Sprintf("add plan-end:: based on deadline %s", task.Deadline.String()),
			Impact: "makes deadline schedulable",
		})
		// Fall through: deadline task is flexible, so extend is skipped
		// below, but spill/reduce/hours may still apply.
	}
	if task.PlanEnd != nil && task.PlanEnd.Before(in.today) {
		out = append(out, addHoursSuggestion(sets, "adds capacity for overdue task", r.TaskID))
		out = append(out, reduceDurationSuggestion(r, task, in.Granularity))
		return out
	}
	if spill := spillSuggestion(sets); spill != nil {
		spill.TaskID = r.TaskID
		out = append(out, *spill)
	}
	if task.PlanEnd != nil && planEndBoundsPlacement(*task.PlanEnd, in.horizonEnd) {
		out = append(out, extendPlanEndSuggestion(r, task, in, sets))
	}
	out = append(out, reduceDurationSuggestion(r, task, in.Granularity))
	out = append(out, addHoursSuggestion(sets, "adds capacity", r.TaskID))
	return out
}

// planEndBoundsPlacement reports whether moving plan-end can add placeable
// slots inside the current scheduling window. When plan-end is already at or
// beyond the horizon, the window (not plan-end) is the limiting bound and an
// extension buys nothing placeable until the window itself moves.
func planEndBoundsPlacement(planEnd, horizonEnd model.CivilDate) bool {
	return planEnd.Before(horizonEnd)
}

func blockedByCycle(blockedBy []string, cycleSet set.Set[string]) bool {
	for _, id := range blockedBy {
		if cycleSet.Has(id) {
			return true
		}
	}
	return false
}

func displayText(t model.Task) string {
	if t.Text != "" {
		return t.Text
	}
	return t.ID
}

func unknownMapSlugs(task model.Task, maps map[string]model.TimeMap) []string {
	var out []string
	for _, slug := range task.TimeMaps {
		if _, ok := maps[slug]; !ok {
			out = append(out, slug)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func reduceDurationSuggestion(r TaskReport, task model.Task, granularity time.Duration) Suggestion {
	shortMin := int64(r.Shortfall) * int64(granularity.Minutes())
	var change string
	if shortMin >= 60 && shortMin%60 == 0 {
		change = fmt.Sprintf("reduce duration by %dh", shortMin/60)
	} else {
		change = fmt.Sprintf("reduce duration by %dm", shortMin)
	}
	return Suggestion{
		TaskID: r.TaskID, Reason: SuggestReduceDuration,
		Change: change, Impact: "fits in current window",
	}
}

func extendPlanEndSuggestion(r TaskReport, task model.Task, in suggestInput, sets timeMapSets) Suggestion {
	days := suggestMinExtendDays
	if r.Shortfall > suggestExtendShortfallThreshold {
		if weekly := sets.eligibleSlots(in.capacity); weekly > 0 {
			need := (r.Shortfall*weeklyDays + weekly - 1) / weekly
			days = min(max(need, suggestMinExtendDays), suggestMaxExtendDays)
		}
	}
	tier := "green"
	if len(task.TimeMaps) > 0 && len(sets.primary) > 0 {
		tier = string(tierByRank(sets.best))
	}
	return Suggestion{
		TaskID: r.TaskID, Reason: SuggestExtendPlanEnd,
		Change: fmt.Sprintf("move plan-end by %d days", days),
		Impact: fmt.Sprintf("adds ~%d days of eligible %s capacity", days, tier),
		Tier:   tier,
	}
}

// addHoursSuggestion names the thinnest weekdays in the eligible set so the
// advice is actionable (issue #3: add/shift hours on specific days).
// Only windows the task may use count: primaries in full plus fallback
// windows at the same tier or worse, the set timeMapSets owns.
func addHoursSuggestion(sets timeMapSets, impact, taskID string) Suggestion {
	days := thinnestWeekdays(sets.mapsForHours(), thinDaysToName)
	var change string
	if len(days) == 0 {
		change = "add Time Map hours on free days"
	} else {
		names := make([]string, 0, len(days))
		for _, d := range days {
			names = append(names, d.String()[:3])
		}
		change = fmt.Sprintf("add Time Map hours on %s", strings.Join(names, ", "))
	}
	return Suggestion{
		TaskID: taskID, Reason: SuggestAddTimeMapHours,
		Change: change, Impact: impact,
	}
}

func thinnestWeekdays(maps []model.TimeMap, n int) []time.Weekday {
	perDay := map[time.Weekday]int{}
	allDays := []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday}
	for _, d := range allDays {
		perDay[d] = 0
	}
	for _, m := range maps {
		for _, w := range m.Windows {
			mins := w.EndMinute - w.StartMinute
			for _, d := range w.Days {
				perDay[d] += mins
			}
		}
	}
	ordered := append([]time.Weekday(nil), allDays...)
	slices.SortFunc(ordered, func(a, b time.Weekday) int {
		return cmp.Or(cmp.Compare(perDay[a], perDay[b]), cmp.Compare(a, b))
	})
	// If every day has generous coverage, still name the thinnest so the
	// suggestion stays specific; the generic fallback is only for no maps.
	if len(maps) == 0 {
		return nil
	}
	if n > len(ordered) {
		n = len(ordered)
	}
	// Only suggest days that are actually thin (<4h) when such days exist;
	// otherwise fall back to the two thinnest to stay actionable.
	var thin []time.Weekday
	for _, d := range ordered {
		if perDay[d] < thinDayMinutes {
			thin = append(thin, d)
		}
	}
	if len(thin) >= 1 {
		if len(thin) > n {
			thin = thin[:n]
		}
		return thin
	}
	return ordered[:n]
}

func spillSuggestion(sets timeMapSets) *Suggestion {
	if len(sets.fallback) == 0 {
		return nil
	}
	hasGreen, hasYellow, hasRed := false, false, false
	for _, d := range sets.fallback {
		for _, w := range d.Windows {
			switch windowTier(w) {
			case model.TierGreen:
				hasGreen = true
			case model.TierYellow:
				hasYellow = true
			case model.TierRed:
				hasRed = true
			}
		}
	}
	// Spill is a heuristic over configured default tiers, not a placement
	// guarantee: busy time or occupancy may still block it, hence "if free".
	switch sets.best {
	case 0: // green task: same or worse
		if hasGreen {
			return &Suggestion{Reason: SuggestSpillToDefaultGreen, Change: "spill to default green", Impact: "uses default green hours if free", Tier: "green"}
		}
		if hasYellow {
			return &Suggestion{Reason: SuggestSpillToDefaultYellow, Change: "spill to default yellow", Impact: "uses default yellow hours if free", Tier: "yellow"}
		}
		if hasRed {
			return &Suggestion{Reason: SuggestSpillToDefaultRed, Change: "spill to default red", Impact: "uses default red hours if free", Tier: "red"}
		}
		return nil
	case 1: // yellow task: yellow or red, never green
		if hasYellow {
			return &Suggestion{Reason: SuggestSpillToDefaultYellow, Change: "spill to default yellow", Impact: "uses default yellow hours if free", Tier: "yellow"}
		}
		if hasRed {
			return &Suggestion{Reason: SuggestSpillToDefaultRed, Change: "spill to default red", Impact: "uses default red hours if free", Tier: "red"}
		}
		return nil
	default: // red task: red only, never yellow/green
		if hasRed {
			return &Suggestion{Reason: SuggestSpillToDefaultRed, Change: "spill to default red", Impact: "uses default red hours if free", Tier: "red"}
		}
		return nil
	}
}
