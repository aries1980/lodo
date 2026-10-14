# Lodo — Concepts Overview

> Status: **draft, in progress.** This page is a visual companion to the glossary in
> [`glossary.md`](./glossary.md). Definitions live there; relationships live here.

## Concept map

How the core terms relate. A Task carries scheduling intent (Plan, Pin, Deadline,
Priority, Duration) and belongs to Time Maps that constrain *which hours of the week*
it may occupy. Lodo turns that into Blocks, which form the Schedule and are projected
onto one Calendar.

```mermaid
flowchart LR
    Task -->|when by| Plan
    Task -->|exactly when| Pin
    Task -->|reference due| Deadline
    Task -->|how long| Duration
    Task -->|ordering| Priority
    Task -->|belongs to| TimeMap["Time Map"]
    Task -->|refuses to cross| Deadline

    TimeMap -->|constrains hours of| Scheduler
    Plan -->|constrains window of| Scheduler
    Pin -->|fixed anchor for| Scheduler

    Scheduler["Scheduler"] -->|produces| Block
    Block -->|collectively form| Schedule
    Block -->|projected as| ManagedEvent["Managed Event"]
    ExternalEvent["External Event"] -.->|read as busy time| Scheduler
```

## Scheduling pipeline

```mermaid
flowchart LR
    LS[("Logseq graph<br/>Tasks = TODO blocks")]
    CFG[/"Lodo config<br/>Time Maps, granularity, window"/]
    CAL[("Google Calendar<br/>busy time + External Events")]
    LODO{{"Lodo CLI<br/><code>schedule</code> / <code>refresh</code>"}}
    DB[("Local state<br/>SQLite")]
    OUT["Managed Events<br/>(write)"]

    LS -->|tasks + properties| LODO
    CFG -->|preferences| LODO
    CAL -->|busy + External Events| LODO
    LODO -->|assignments + event IDs| DB
    LODO -->|create / move Blocks| OUT
    DB -->|reconcile by event ID| LODO
    OUT -.->|projection of| CAL
```

## Status

All major v0.1 decisions are resolved. What is deferred, and the v0.2+ roadmap, is in
[`scope.md`](./scope.md).
