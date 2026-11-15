---
title: "Reads fan out with a bounded number of workers"
weight: 240
---

> Status: **accepted.**

A first run spent most of its wall time in serial HTTP. `run.LoadGraph` fetched one
page tree per round-trip, plus page properties for pages that could pass properties on.
`calendar.MultiClient.Busy` read each shared calendar in turn. Both clients are
stateless, and `http.Client` plus the OAuth token source are safe for concurrent use.
The scheduling window and the page list were the only things the reads needed, so the
calls were independent.

Independent reads now fan out through `internal/fanout.Do[T]`. It runs at most four
calls at once with `sync.WaitGroup.Go`, stores each result by index, and returns the
results in index order. Callers assemble their maps and reports after the fan-out:
`LoadGraph` fills `Input.Trees` and page properties in page order, and `Busy` appends
audit rows and warnings in config order. No goroutine appends to a shared slice or map.
The write calendar is still read before the read calendars, and calendar writes never
run concurrently.

Four workers is the low end of the useful range. It cuts a large graph's serial
round-trips without turning a desktop app or a Google quota into a load target. The
number is a package constant, not configuration: a knob would need validation, docs,
and a reason to be tuned per run, and the endpoints do not ask for one.

On the first failure the helper cancels the outstanding calls, waits for every started
call, and returns the lowest-index failure that is not a cancellation caused by that
failure. A single failure keeps the sequential error wording. If a later index fails
first and cancels an earlier read, the earlier read's cancellation is not reported in
place of the real failure. A cancelled parent context is returned as its own error.

Unbounded goroutines were rejected: a few hundred pages would become a few hundred
simultaneous calls to the app. Sequential reads were the problem being solved.
`golang.org/x/sync/errgroup` was rejected as a new dependency that still leaves callers
assembling index-addressed results; its completion-order error is not the order the
sequential code reported. Duplicating the worker loop in both packages was rejected
because the cancellation and error-selection rules are subtle enough to drift.
Fanning out across graph sources was rejected for this change: each graph is a separate
Logseq window and client ([ADR 0021](./0021-each-graph-is-its-own-logseq-client.md)),
and the graph list's order also decides cross-graph tie-breaks.

Consequences: `logseq.Client` and `calendar.Client` are documented as safe for
concurrent use, and every future adapter (the DB graph, another calendar provider) must
keep that contract. Output and `schema_version` are unchanged: the reads are
concurrent, but the assembled order is the order the serial code produced. Graph
sources and the graph-then-calendar pipeline stay sequential; overlapping those is a
separate change. The Logseq desktop app's tolerance for four concurrent reads is
bounded by the worker count and by cancellation on the first failure; a serial
implementation remains one constant away if an app version misbehaves.
