package planner

import (
	"time"

	"github.com/aries1980/lodo/internal/grid"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/set"
)

// planRun is the state one Plan call threads through its phases. It replaces
// the loose reportInput/decisionsInput/placementScope bundles: every phase
// reads and writes this one value, and Plan reads as a pipeline.
type planRun struct {
	in Input
	g  *grid.Grid
	b  *builder

	today      model.CivilDate
	horizonEnd model.CivilDate

	plans      []*taskPlan
	pins       []*taskPlan
	candidates []*taskPlan
	lookup     planLookup
	maps       map[string]model.TimeMap
	ends       map[string]time.Time

	order     []*taskPlan
	orderSet  set.Set[string]
	cycleSet  set.Set[string]
	unknown   map[string][]string
	blocked   map[string]Reason
	blockedBy map[string][]string

	report Report
	blocks []model.Block
	expls  []Explanation
}

func newPlanRun(in Input) (*planRun, error) {
	g := grid.Build(in.Now, in.Location, in.Granularity, in.WindowDays)
	busy, err := buildBusy(in.Busy, g)
	if err != nil {
		return nil, err
	}

	dispositions, err := indexDispositions(in.Dispositions)
	if err != nil {
		return nil, err
	}

	plans, err := buildPlans(in.Tasks, dispositions, in.Granularity)
	if err != nil {
		return nil, err
	}

	build := &builder{g: g, busy: busy, external: busy.Clone()}
	build.pinMask = grid.NewMask(g.Len())
	build.pinnedMask = grid.NewMask(g.Len())

	today := model.CivilDateOf(in.Now.In(in.Location))
	return &planRun{
		in:         in,
		g:          g,
		b:          build,
		today:      today,
		horizonEnd: today.AddDays(in.WindowDays),
		plans:      plans,
	}, nil
}

// emitPins places Pins and user-moved Blocks at their exact authored time,
// then snapshots the masks the explain breakdowns use. A Pin with
// dependencies stays at its authored time; the order violation is reported
// after placement.
func (r *planRun) emitPins() {
	r.pins = collectPins(r.plans)
	emitPins(r.pins, r.b)
	r.b.pinMask = r.b.busy.AndNot(r.b.external)
	emitPinned(r.plans, r.b, r.in.Granularity)
	r.b.pinnedMask = r.b.busy.AndNot(r.b.external).AndNot(r.b.pinMask)
	r.b.baseBusy = r.b.busy.Clone()
}

// orderCandidates builds the lookup and dependency edges, then orders the
// search: topological over dependencies first (deps before dependents, even
// across time-bound/flexible), the existing comparator as tie-break among
// unrelated Tasks. Containers never enter the order.
func (r *planRun) orderCandidates() {
	r.lookup = indexPlans(r.plans)
	r.candidates = collectCandidates(r.plans)
	r.unknown = findUnknownDeps(r.candidates, r.lookup)
	r.order, r.cycleSet = topoOrder(r.candidates, r.lookup, r.unknown)

	r.orderSet = set.New[string]()
	for _, plan := range r.order {
		r.orderSet.Add(plan.task.ID)
	}

	r.maps = indexTimeMaps(r.in.TimeMaps)
	r.ends = initialEnds(r.pins, r.plans)
}

// schedule finishes the run: shortfall report first (decisions consume it),
// then the explain trace assembled from placement facts.
func (r *planRun) schedule() Schedule {
	r.report = r.buildReport()
	r.blocks, r.expls = r.b.sorted()

	schedule := Schedule{
		Blocks:       r.blocks,
		Report:       r.report,
		Explanations: r.expls,
		Decisions:    r.buildDecisions(),
	}
	schedule.Suggestions = Suggest(r.in, schedule, r.cycleSet)
	return schedule
}
