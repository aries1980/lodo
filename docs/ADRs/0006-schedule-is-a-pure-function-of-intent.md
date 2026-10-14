# The Schedule is a pure function of current intent

The Schedule is computed from scratch on every run: the planner is a pure function of the
Tasks, Time Maps, busy time, Pins, Calendar dispositions and config that exist *now*. The
previous Schedule is never an input, and the SQLite store is a record of identity, input
snapshots and event bindings — not a scheduling input. The two hard guarantees in
[`scheduling.md`](../specs/scheduling.md) then hold by construction: the same inputs
produce an identical Schedule, and a run with no changed inputs moves nothing. A wiped
store schedules identically to a populated one.

The price is that "minimal-churn displacement" is an *emergent* property, not an enforced
one. Priority ordering plus front-loading keep unrelated Blocks put in the normal case,
but an external event appearing can still shift a flexible tail. The alternative — feeding
the previous Blocks back in as soft preferences — would let churn be asserted, at the cost
of the Schedule depending on what happens to be in the store, so the same notes could
schedule differently on a fresh machine.

Retention is additive if it is ever wanted: the planner's input gains a `previous` field
without disturbing any other boundary. The decision is to start without it.
