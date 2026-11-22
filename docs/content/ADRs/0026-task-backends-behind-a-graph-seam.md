---
title: "Task backends behind a graph seam"
weight: 260
---

> Status: **accepted.**

Every task backend (Logseq, Keep Workspace, Keep Personal, a future
SQLite store, …) implements one neutral seam: `internal/graph` (`Page`,
`Block`, `Client`, `Walk`). `run.Source` carries a `graph.Client` plus
identity (`Name`, `Order`, `DefaultTimeMap`, `APIURL`, `Backend`);
`resolve.Input`, `run.LoadGraph`, `tui.Load`, the Time Maps bootstrap and
the probe speak only that seam. `internal/logseq` is one implementation
of it (its `Page`/`Block`/`Client` are aliases, so existing tests compile
unchanged); `internal/keep` holds the shared Keep mapping with the
`workspace` and `personal` transports beneath it.

## Why a 7-method client, not per-backend resolution

The alternative was one resolver input per backend, each building
`resolve.Input` its own way. That duplicates the read strategy (page
enumeration, tree assembly, Time Maps validation) per backend and lets
the strategies drift. The 7-method client (`Ping/Pages/Page/Tree` plus
the four writes) keeps what-to-read with the caller and faithful
transport with the adapter, so `resolve`, `planner`, `reconcile` and the
store never learn a backend exists. A new backend is one package
implementing seven methods plus config/credential plumbing — no resolver
changes. The cost is paid by backends without hierarchy (Keep, SQLite):
they synthesize flat trees and accept the unsupported list in
[keep-integration.md](../specs/keep-integration.md#unsupported-on-keep).

## Why split by account type, not by API

The two Keep access paths share a mapping but nothing else: OAuth +
official REST versus master token + reverse-engineered sync. One
"Keep backend" with an internal switch would still need both transports,
both credential shapes and both failure modes, so the split is explicit:
`internal/keep/workspace` and `internal/keep/personal`, selected per
source by `backend: keep-workspace | keep-personal`. Backend ids name the
account type because that is the user's decision; the protocol follows.

## Why the Time Maps note is text while tasks are checklists

Keep list items cannot nest, and Time Map windows must be children of
their map block for the resolver. The Time Maps note is therefore an
outline text note whose indentation the adapter parses back into a tree —
adapter-local emulation, zero resolver changes. Task notes stay
checklists so the Keep UI checkbox remains the task marker.

## Consequences

- `run.Source.Backend` names the adapter in diagnostics; probe messages
  and `keep.retry` warn records are backend-aware, Logseq wording
  byte-identical.
- Credentials: official Keep reuses the Google grant (the `keep` scope
  joins the `auth google` flow); personal Keep uses `LODO_KEEP_TOKEN*`
  like Logseq tokens. Sync roots are machine-bound config
  (`keep.sync_url`), never flags.
- No relation to ADR 0012: that record rejects Google *Tasks* as a
  second projection; Keep here is a task *source*, and the calendar
  remains the sole projection.
- Live verification of the official wire and the personal sync handshake
  against real Google accounts is tracked follow-up work; both adapters
  are pinned by httptest-fake suites, and the personal one fails closed
  without a configured `sync_url`.
