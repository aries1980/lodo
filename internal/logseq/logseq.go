// Package logseq reads a Logseq graph over its HTTP API (ADR 0002: HTTP only,
// read-only).
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
	"context"
)

// Page is one page of the graph.
type Page struct {
	// Name is the page's canonical name, e.g. "2026-10-15" or "projects/mgi".
	Name string `json:"name"`
	// UUID is the page's id; empty for pages Logseq has not materialised.
	UUID string `json:"uuid,omitempty"`
	// Properties are the page's parsed properties (the frontmatter), or nil.
	// They are the inheritance source for Tasks on the page.
	Properties map[string]any `json:"properties,omitempty"`
}

// Block is one block of a block tree. Tree endpoints return top-level blocks
// with their children nested recursively.
type Block struct {
	// UUID identifies the block across runs; it is a Task's identity.
	UUID string `json:"uuid"`
	// Content is the block's raw markdown: the text, a task marker, property
	// lines and logbook lines.
	Content string `json:"content"`
	// Marker is the block's task marker ("TODO", "DOING", …) present only
	// when the block carries one.
	Marker string `json:"marker,omitempty"`
	// Properties are the block's parsed property values, as the app typed
	// them (strings, numbers, booleans, lists).
	Properties map[string]any `json:"properties,omitempty"`
	// PropsText are the same properties' raw text values, when the app
	// exposes them; parsed Properties take precedence.
	PropsText map[string]any `json:"propertiesTextValues,omitempty"`

	// Children are the nested child blocks in sidecar order.
	Children []*Block `json:"children,omitempty"`
}

// Client reads from one Logseq app instance over the HTTP API. One adapter
// per graph flavour (logseq-integration.md); the file-based-graph adapter
// below is the only one in v0.1.
type Client interface {
	// Pages lists every page in the graph.
	Pages(ctx context.Context) ([]*Page, error)
	// Page returns one page's properties, or nil when the page does not
	// exist.
	Page(ctx context.Context, name string) (*Page, error)
	// Tree returns a page's block tree, top-level blocks with children
	// nested; nil when the page is empty.
	Tree(ctx context.Context, name string) ([]*Block, error)
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
