// Package personal is the keep-personal backend: the token-authenticated
// sync transport for free personal @gmail.com accounts, which the
// official API rejects with 400 invalid_scope (keep-integration.md,
// ADR 0026). Experimental.
//
// The sync protocol is reverse-engineered and endpoint shapes drift on
// Google-side changes, so the transport root is explicit configuration
// (keep.sync_url), never a guess baked in here. The mapping is the shared
// one in the parent keep package; the note shapes are the official ones,
// spoken through the workspace adapter until the personal sync wire is
// pinned against live Google (issue #29). Every call refuses before any
// I/O while no sync_url is configured, with an error that names the docs
// instead of dialing a guessed host.
package personal

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/aries1980/lodo/internal/graph"
	"github.com/aries1980/lodo/internal/keep"
	"github.com/aries1980/lodo/internal/keep/workspace"
)

// Client is the personal-account adapter. It wraps the shared note
// mapping; the sync wire beneath is experimental (see checkConfigured).
type Client struct {
	base  string
	inner *workspace.Client
}

// NewClient starts one personal-account adapter. masterToken is the token
// minted per keep-integration.md (personal setup); baseURL is the sync
// endpoint root (keep.sync_url) — empty means unconfigured, and every
// call then fails with an actionable error instead of dialing a guessed
// host. hc nil means the default transport with DefaultTimeout.
func NewClient(masterToken, baseURL, label string, hc *http.Client) (*Client, error) {
	if strings.TrimSpace(masterToken) == "" {
		return nil, fmt.Errorf("keep personal: no token; mint one per keep-integration.md (personal setup) or set %s", keep.EnvKeepToken)
	}
	if hc == nil {
		hc = &http.Client{Timeout: workspace.DefaultTimeout}
	}
	inner, err := workspace.NewClient(hc, firstNonEmpty(baseURL, workspace.KeepBase), label)
	if err != nil {
		return nil, err
	}
	return &Client{base: strings.TrimRight(strings.TrimSpace(baseURL), "/"), inner: inner}, nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// errUnconfigured is the fail-closed error when no sync_url is set:
// dialing a guessed Google host would be worse than refusing.
func errUnconfigured() error {
	return fmt.Errorf("keep personal: no sync_url configured; the personal-account sync transport is experimental — see keep-integration.md (personal setup) and issue #29 for the current endpoint")
}

// checkConfigured refuses before any I/O when the transport root is
// unset. A baseURL equal to the official root through this adapter is a
// configuration error: use the keep-workspace backend with an OAuth grant
// instead of a master token against it.
func (c *Client) checkConfigured() error {
	if c.base == "" || c.base == workspace.KeepBase {
		return errUnconfigured()
	}
	if _, err := url.Parse(c.base); err != nil {
		return fmt.Errorf("keep personal: sync_url: %w", err)
	}
	return nil
}

// Ping probes whether the sync transport answers at all.
func (c *Client) Ping(ctx context.Context) error {
	if err := c.checkConfigured(); err != nil {
		return err
	}
	return c.inner.Ping(ctx)
}

// Pages lists every visible note as a page.
func (c *Client) Pages(ctx context.Context) ([]*graph.Page, error) {
	if err := c.checkConfigured(); err != nil {
		return nil, err
	}
	return c.inner.Pages(ctx)
}

// Page returns one note's page, or nil when no note carries the title.
func (c *Client) Page(ctx context.Context, uuid, name string) (*graph.Page, error) {
	if err := c.checkConfigured(); err != nil {
		return nil, err
	}
	return c.inner.Page(ctx, uuid, name)
}

// Tree returns a note's block tree; nil when the note is missing or
// holds no blocks.
func (c *Client) Tree(ctx context.Context, uuid, name string) ([]*graph.Block, error) {
	if err := c.checkConfigured(); err != nil {
		return nil, err
	}
	return c.inner.Tree(ctx, uuid, name)
}

// CreatePage creates a text note titled name.
func (c *Client) CreatePage(ctx context.Context, name string) (*graph.Page, error) {
	if err := c.checkConfigured(); err != nil {
		return nil, err
	}
	return c.inner.CreatePage(ctx, name)
}

// AppendBlock appends one top-level block to a note.
func (c *Client) AppendBlock(ctx context.Context, page, content string) (*graph.Block, error) {
	if err := c.checkConfigured(); err != nil {
		return nil, err
	}
	return c.inner.AppendBlock(ctx, page, content)
}

// UpdateBlock replaces a block's content in place.
func (c *Client) UpdateBlock(ctx context.Context, uuid, content string) error {
	if err := c.checkConfigured(); err != nil {
		return err
	}
	return c.inner.UpdateBlock(ctx, uuid, content)
}

// InsertBlock inserts a block as the last child of its parent (top-level
// on list notes, which have no nesting).
func (c *Client) InsertBlock(ctx context.Context, parentUUID, content string) (*graph.Block, error) {
	if err := c.checkConfigured(); err != nil {
		return nil, err
	}
	return c.inner.InsertBlock(ctx, parentUUID, content)
}

// SetBlockProperty sets one block property.
func (c *Client) SetBlockProperty(ctx context.Context, uuid, key string, value any) error {
	if err := c.checkConfigured(); err != nil {
		return err
	}
	return c.inner.SetBlockProperty(ctx, uuid, key, value)
}
