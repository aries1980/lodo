---
title: Lodo
---

Lodo is a personal, local-first scheduler for a Logseq TODO list. It reads Tasks from a
Logseq graph, decides *when* to work on them, and projects the resulting schedule onto
Google Calendar.

> Status: **early implementation.** The Go binary is under construction from the
> specification; this site holds the specification and the decisions behind it.

Lodo is a CLI you invoke; cron calling `lodo schedule` is the intended deployment.
The Logseq graph is the single source of truth for intent, local SQLite state is the
source of truth for the Schedule, and the calendar is a projection of it.

- [Specifications](specs/): the language, concepts, and behaviour of Lodo. Start with the
  [Glossary](specs/glossary.md).
- [Decision Records](adrs/): the decisions worth remembering, with reasons.
