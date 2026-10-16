---
title: "Time Maps"
weight: 50
---


> Status: **draft (v0.1).** Definition of the term lives in [`glossary.md`](./glossary.md).

A Time Map is a named, weekly-recurring pattern of hours in which the user is willing to
do a particular category of work. It answers *which hours of the week*, independent of
*when by* (Plan) and *exactly when* (Pin).

## Authoring

Time Maps are authored in the Logseq graph, on a page named **`Time Maps`** (see
[ADR 0003](../ADRs/0003-time-maps-in-graph-format.md)).

```markdown
- Deep Work
  - Mon-Fri 09:00-11:00
  - Sat 10:00-12:00 yellow
- Errands
  - Sat 10:00-13:00
- Weekends
  - Sat-Sun 08:00-20:00 yellow
```

## Schema

- **One top-level block = one Time Map.** The block's text is the map's name. The name is
  slugified, case-insensitively, into a key (`Deep Work` → `deep-work`).
- **Each child block = one window.** A window is a human-readable line: days, then a
  start–end time, then an optional trailing tier word.
- A map with no child blocks is empty and schedules nothing.

### Window grammar

```text
window := <days> <HH:MM>-<HH:MM> [tier]
days   := <day> | <day>-<day> | <day>,<day>,...
day    := Mon | Tue | Wed | Thu | Fri | Sat | Sun
tier   := green | yellow | red        ; optional; absent = green
```

- `Mon-Fri 09:00-11:00` — weekday mornings.
- `Mon,Wed,Fri 18:00-19:00` — listed days.
- `Sat-Sun 08:00-20:00 yellow` — a tiered weekend window.
- A window lies within one day: its start is before its end. A window crossing midnight
  is reported as unparseable, never guessed at.

### Tiers

A window's tier is `green`, `yellow`, or `red`. An absent tier word is green.
Placement fills green first, spills into yellow, then red
([ADR 0014](../ADRs/0014-preference-tiers-before-the-comparator.md)). A slot covered by
two windows takes the better tier. A Block stays inside one tier. Hours that fit in
none of the three are a shortfall. Red is a last resort, not a forbidden band.

## Assignment

- A Task may belong to one or more Time Maps, declared on the Task (or inherited from its
  page) as `time-map::`. Multiple maps are comma-separated: `time-map:: deep-work, weekends`.
- A Task is scheduled **only** inside the union of its maps' windows.
- **Default map (proposed):** the map block may carry `default:: true`. A Task with no
  `time-map::` (direct or inherited) uses the default map. Exactly one default must exist.

## Semantics (v0.1)

- Windows recur weekly and are **absolute local times**: they stay 09:00–11:00 in the
  user's current timezone, mirroring SkedPal's stated behaviour.
- Maps may overlap. A Task with several maps draws on the union of their windows;
  where two tiers cover one slot, the better tier wins. Competition for the same
  hours between Tasks is resolved by Priority.
- Windows are intersected with the Schedule's busy time (External Events and Corporate
  Pins) and with the Plan window; a Task is placed only in hours that satisfy all three.

## Deferred

- Amending a map with calendar events (SkedPal's "amend with calendar events").
- Per-week customisation of hours.
