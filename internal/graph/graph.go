// Package graph defines the backend-neutral task-source seam (ADR 0026).
//
// Every task backend (Logseq over its HTTP API, Google Keep via the
// official Workspace API or the unofficial personal-account sync protocol,
// a future SQLite store, …) implements Client: dumb query primitives with
// no domain logic. What to read (all pages, page properties, block trees)
// is decided by the caller (internal/run); the adapter's only job is
// faithful transport. Anything unparseable is left for the caller to
// report and skip, never guessed at.
//
// Page and Block are the canonical shapes. Backend adapters that have no
// native hierarchy (Keep, SQLite) synthesize them: one note or row set
// becomes one Page, one item or row becomes one Block (see
// internal/keep for the checklist-item-per-task mapping).
package graph

import (
	"context"
)

// Page is one page of the source: a named group of blocks. For Logseq it
// is a graph page; for Keep it is one note (selected by label); for a
// future SQLite backend it would be one task list.
type Page struct {
	// Name is the page's canonical name, e.g. "2026-10-15" or "Time Maps".
	Name string `json:"name"`
	// UUID is the page's id; empty when the backend has none.
	UUID string `json:"uuid,omitempty"`
	// Properties are the page's parsed properties, or nil. They are the
	// inheritance source for Tasks on the page.
	Properties map[string]any `json:"properties,omitempty"`
}

// Block is one block of a block tree. Tree endpoints return top-level
// blocks with their children nested recursively. Backends without native
// nesting return a flat tree (children nil); the resolver treats that as
// a page with no containers or sequential chains.
type Block struct {
	// UUID identifies the block across runs; it is a Task's identity.
	// Backends without stable per-item ids synthesize one (e.g. Keep uses
	// noteID/itemID) that must stay stable across runs.
	UUID string `json:"uuid"`
	// Content is the block's raw markdown: the text, a task marker,
	// property lines and logbook lines.
	Content string `json:"content"`
	// Marker is the block's task marker ("TODO", "DOING", …) present only
	// when the block carries one.
	Marker string `json:"marker,omitempty"`
	// Properties are the block's parsed property values, as the backend
	// typed them (strings, numbers, booleans, lists).
	Properties map[string]any `json:"properties,omitempty"`
	// PropsText are the same properties' raw text values, when the
	// backend exposes them; parsed Properties take precedence.
	PropsText map[string]any `json:"propertiesTextValues,omitempty"`

	// Children are the nested child blocks in sidecar order.
	Children []*Block `json:"children,omitempty"`
}

// Client reads from one task backend. One adapter per backend flavour.
// Implementations must be safe for concurrent use: callers may fetch
// independent pages at the same time, and the adapter must correlate the
// responses without shared mutable state.
//
// Reads are the whole loop, with one exception: the prompted bootstrap of
// a missing Time Maps page (internal/cli), which creates the page and its
// recommended defaults through the same adapter.
type Client interface {
	// Ping probes whether the backend answers at all. It is the
	// fail-early gate the CLI runs before calendar/store I/O.
	Ping(ctx context.Context) error
	// Pages lists every page in the source.
	Pages(ctx context.Context) ([]*Page, error)
	// Page returns one page's properties, or nil when the page does not
	// exist. uuid is the page id Pages reported, when the caller has
	// one; adapters that address pages by id use it to fetch directly,
	// and fall back to name when it is empty.
	Page(ctx context.Context, uuid, name string) (*Page, error)
	// Tree returns a page's block tree, top-level blocks with children
	// nested; nil when the page is empty or missing. uuid is as for
	// Page.
	Tree(ctx context.Context, uuid, name string) ([]*Block, error)

	// CreatePage creates a page with no blocks yet.
	CreatePage(ctx context.Context, name string) (*Page, error)
	// AppendBlock appends one top-level block to a page.
	AppendBlock(ctx context.Context, page, content string) (*Block, error)
	// UpdateBlock replaces a block's content in place.
	UpdateBlock(ctx context.Context, uuid, content string) error
	// InsertBlock inserts a block as the last child of its parent.
	InsertBlock(ctx context.Context, parentUUID, content string) (*Block, error)
	// SetBlockProperty sets one block property (e.g. default:: true).
	SetBlockProperty(ctx context.Context, uuid, key string, value any) error
}

// Walk visits every block in depth-first, top-to-bottom display order —
// the order the Outline shows and the source of Task.OutlineOrder.
func Walk(blocks []*Block, visit func(*Block) error) error {
	for _, b := range blocks {
		if err := visit(b); err != nil {
			return err
		}
		if err := Walk(b.Children, visit); err != nil {
			return err
		}
	}
	return nil
}
