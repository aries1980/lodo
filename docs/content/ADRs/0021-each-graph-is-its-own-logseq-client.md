---
title: "Each graph is its own Logseq client"
weight: 210
---

> Status: **accepted.** The config shape lives in
> [configuration.md](../specs/configuration.md#multiple-graphs).

Logseq's HTTP API serves the graph open in that window. The calls Lodo uses take no
repo argument. Several graphs therefore means several Logseq windows, each with its
own API URL and token. Lodo reads Tasks from all of them and reads the canonical
Time Maps page from the first graph only. Another graph's Time Maps page contributes
no maps.

`depends::` and `order::` resolve inside one graph. A reference that names a block in
another graph is an unknown dependency and blocks. A block ref is an unqualified UUID
([ADR 0020](./0020-task-identity-is-the-block-uuid.md)), so it cannot point at a
second graph without guessing.

One shared client, pointed at a repo argument Lodo would have to invent, was
rejected. The API does not offer that argument, and a client aimed at the wrong
window would read the wrong graph and report nothing. Merging Time Maps from every
graph was rejected. Two pages named `Time Maps` can disagree, and the user would not
be able to tell which definition won.

A graph that is not running fails the run at that endpoint. When two Tasks still tie
after the usual keys, the configured graph list breaks the tie, then outline order
([ADR 0022](./0022-outline-order-is-a-priority.md)).
