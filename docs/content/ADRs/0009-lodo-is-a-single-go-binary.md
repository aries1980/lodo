---
title: "Lodo is a single Go binary"
weight: 90
---


Lodo is implemented in Go and ships as one static binary, invoked by cron or by hand;
there is no interpreter, no virtualenv and no daemon. Go's
`google.golang.org/api/calendar/v3` and `golang.org/x/oauth2` cover the loopback OAuth
flow and event CRUD, `modernc.org/sqlite` is a pure-Go driver so the build needs no cgo
and cross-compiles cleanly, and `time.LoadLocation` with embedded `tzdata` gives correct
DST behaviour for the absolute-local-time semantics of Time Maps.

The rejected alternatives were Python — quickest to a first working schedule, but cron
plus a virtualenv plus dependency drift is friction a set-and-forget personal tool should
not carry — and Rust, which would strengthen the scheduler's correctness guarantees but
requires more hand-rolled calendar and OAuth plumbing to reach the same result.
