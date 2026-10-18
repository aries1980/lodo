---
title: "Scope"
weight: 30
---


> Status: **draft (v0.1).**

## In scope for v0.1

The core loop: **read Tasks from Logseq → decide when → write Blocks to Google Calendar → reconcile on refresh.**

- **Single-user, self-hosted, local-first.** No accounts, no multi-tenancy, no server. A CLI you invoke; cron calling `lodo schedule` is the intended deployment.
- **Logseq as the source of truth for intent**, reached over the HTTP API, **read-only except one prompted bootstrap**: when the Time Maps page is missing, Lodo offers to create it with recommended defaults on explicit confirmation. File-based graph today; the DB graph after the planned upgrade.
- **Tasks** = `TODO`/`DOING` blocks with an explicit `duration::`. Duration is the gate.
- **Plan** = optional `plan-start::`/`plan-end::` date bounds; absent bounds mean flexible ("as soon as possible").
- **Pin** via Logseq's native `scheduled::`; **Deadline** via native `deadline::`; **Priority** via native `priority::` (A/B/C).
- **Dependencies** via `depends:: ((uuid))` (strict finish-to-start, AND, full-gate) plus SkedPal-parity nesting: containers (parents with Task children unscheduled), `order:: sequential|parallel` chains, block-cascade inheritance, DONE-parent suppression.
- **Time Maps** authored in the graph on a configurable page, in a line-based format, with green/yellow/red preference tiers.
- **Scheduling**: front-loading, deterministic, minimal-churn displacement; a no-change run is a strict no-op. Highlights the rules in [`scheduling.md`](./scheduling.md).
- **Infeasibility** is reported, never silently resolved: no Auto-Defer, no plan or Time-Map violation. Partial placement is allowed with a reported shortfall.
- **Google Workspace Calendar** only: one write calendar plus shared read calendars for
  busy time. Managed Events identified by private extended properties; moves become
  Pinned Blocks, deletes stand the Task down, everything else is busy time.
- **Configuration** in a file, secrets in a `0600` credentials file; no secrets in the graph.
- **CLI**: `check`, `forecast`, `schedule`, `refresh`, `auth google`.

## Out of scope (roadmap, not v0.1)

Each of these is a deliberate deferral, not an oversight. They are roughly ordered by how
soon they are likely to matter.

- **Bundling** — packing small Tasks into shared events.
- **Minimum Block Length** as a user property — v0.1 leans on granularity plus "largest-first" splitting.
- **Buffers** and **Auto-Defer**.
- **Zones and budgets** — the zero-sum balancing layer.
- **Recurring Tasks and habits.**
- **Sequential one-at-a-time gating** (sequential chains schedule upfront in order).
- **Multiple Plans per Task.**
- **Suggestions** (SkedPal's proposal of a more realistic date).
- **Outlook and iCloud** calendars.
- **A read-only projection into the graph** (`lodo-*` mirror).
- **NLP** parsing of Task titles.
- **Any Lodo UI** — Logseq is the interface; a Status Tracker / board / calendar views are not built.
