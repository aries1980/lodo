---
title: "Glossary"
weight: 10
---


Lodo is a personal, local-first task scheduler. It reads tasks and their scheduling
preferences from a Logseq graph, decides when to work on them, and projects that
decision onto a calendar.

See also the visual [`overview.md`](./overview.md).

## Language

**Task**:
A unit of work the user wants scheduled. A plain note is not a Task — a Task is a
line the user has given scheduling input.
_Avoid_: item, chore, action, entry

**Block**:
One contiguous interval of scheduled time belonging to a single Task. A Task whose
estimated effort exceeds the time available in one sitting may have several Blocks.
_Avoid_: slot, event, segment, session
_Note_: "block" is only ever the noun here. The scheduling *method* that gives a
Task its own dedicated calendar entry is called Time Blocking, never "a block".

**Pinned Block**:
A Block that Lodo placed but the user subsequently moved by hand in the calendar.
Lodo keeps it at the user-chosen time and span, and schedules other work around it.
_Avoid_: locked block, fixed block, override
_Note_: The time *and span* are pinned, not the title — a Pinned Block stays where
the user put it, at the length they gave it, and adopts the Task's current text on
recompute. It persists until its event is deleted — surviving unrelated changes
to the Task's inputs — and is distinct from a **Pin**, which the user authors in Logseq.

**Stood down**:
A Block whose Managed Event the user deleted in the calendar. Lodo will not recreate
that Block until the Task's own inputs change; the Task's surviving Blocks stay projected.
_Avoid_: paused, held, cancelled

**Managed Event** / **External Event**:
A calendar entry Lodo created and may move (Managed) versus one the user created
themselves, which Lodo only reads as busy time and never moves (External).
_Avoid_: owned event, foreign event, personal event

**Calendar**:
The single external calendar (Google Workspace) onto which Lodo projects the Schedule.
Time Maps partition the *week*; they are not calendars.
_Avoid_: agendas, multiple calendars

**Plan**:
The user's intent about *when* a Task should happen, expressed loosely (a window, or
"as soon as possible") rather than as a fixed time.
_Avoid_: schedule, appointment, slot

**Pin**:
A specific date and time the user has fixed a Task to, meaning "put it here and do
not move it". A Pin is authored by the user; Lodo never creates one.
_Avoid_: fixed event, appointment, booking

**Deadline**:
A reference due date on a Task, for the user's own benefit and for filtering. A
Deadline never by itself causes a Task to be scheduled — only a Plan does.
_Avoid_: due date, plan end, target date

**Priority**:
A Task's rank relative to other Tasks, deciding who gets scarce hours when they compete.
It is coarse and deliberately not a total order: ties are broken by Plan urgency and
then by outline order.
_Avoid_: importance, weight, rank

**Time Map**:
A named, weekly-recurring pattern of hours in which the user is willing to do a
particular category of work (e.g. Deep Work, Errands, Weekends). A Task belongs to
one or more Time Maps and is scheduled only inside their hours.
_Avoid_: availability, calendar, working hours
_Note_: Plan says *when by*; Time Map says *which hours of the week*; Pin says
*exactly when*. They are orthogonal and must not be conflated.

**Schedule**:
The concrete set of Blocks Lodo has produced for Tasks. The Schedule is the result
of planning; it is not itself a Plan.
_Avoid_: calendar, agenda

**Scheduling granularity**:
The quantum of the Schedule. Every Block is a whole number of granularity units, and a
Task shorter than one unit occupies a single unit.
_Avoid_: slot size, resolution, tick

**Scheduling window**:
The rolling horizon, in days, within which Lodo produces and reconciles the Schedule.
Nothing beyond it is scheduled or synced.
_Avoid_: horizon, lookahead, range
