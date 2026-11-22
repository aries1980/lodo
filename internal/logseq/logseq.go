// Package logseq talks to a Logseq graph over its HTTP API (ADR 0002: HTTP
// only). It is one implementation of the backend-neutral seam in
// internal/graph (ADR 0026): Page, Block and Client are canonical there,
// and this package re-exports them as aliases so existing callers and
// tests keep compiling while new code imports internal/graph directly.
//
// Reads are the common path. The two writes are the Time Maps bootstrap
// (ADR 0015) and a validated TUI block save (ADR 0017).
//
// The Client is an interface with one adapter per graph flavour: the
// file-based graph today, the DB graph after Logseq's planned upgrade. The
// adapter must stay a swap, not a rewrite of everything above it, so it
// exposes dumb query primitives with no domain logic: what to read (all
// pages, page properties, block trees) is decided by the caller, and the
// adapter's only job is faithful transport.
//
// Anything unparseable is left to the caller to report and skip, never
// guessed at (logseq-integration.md).
package logseq

import (
	"github.com/aries1980/lodo/internal/graph"
)

// Page is one page of the graph. Canonical definition: graph.Page.
type Page = graph.Page

// Block is one block of a block tree. Canonical definition: graph.Block.
type Block = graph.Block

// Client reads from one Logseq app instance over the HTTP API. Canonical
// definition: graph.Client — one adapter per backend flavour
// (logseq-integration.md, keep-integration.md); the file-based-graph
// adapter below is the only Logseq one in v0.1.
//
// Implementations must be safe for concurrent use: callers may fetch
// independent pages at the same time, and the adapter must correlate the
// responses without shared mutable state.
//
// Reads are the whole loop, with one exception: the prompted bootstrap of
// a missing Time Maps page (internal/cli), which creates the page and its
// recommended defaults through the same adapter. Lodo never edits the
// user's Tasks, Blocks, or properties.
type Client = graph.Client

// Walk visits every block in depth-first, top-to-bottom display order —
// the order the Outline shows and the source of Task.OutlineOrder.
// Canonical definition: graph.Walk.
func Walk(blocks []*Block, visit func(*Block) error) error {
	return graph.Walk(blocks, visit)
}
