package logseq

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultTimeout bounds one API call. The app runs locally; a call that sits
// on a wall for half a minute has stopped answering.
const DefaultTimeout = 30 * time.Second

// apiClient is the file-based-graph adapter: a Logseq desktop app serving
// its HTTP API (ADR 0002). Every call is POST {base}/api with a {"method",
// "args"} body and a Bearer token.
type apiClient struct {
	base  *url.URL
	token string
	hc    *http.Client
}

// NewClient starts one adapter against the Logseq app's API endpoint (the
// config's logseq.api_url) using the credentials.json token. hc nil means
// the default transport with DefaultTimeout.
func NewClient(apiURL, token string, hc *http.Client) (*apiClient, error) {
	u, err := url.Parse(apiURL)
	if err != nil {
		return nil, fmt.Errorf("logseq client: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("logseq client: %q: must be an absolute http(s) URL", apiURL)
	}
	if hc == nil {
		hc = &http.Client{Timeout: DefaultTimeout}
	}
	return &apiClient{base: u, token: token, hc: hc}, nil
}

// request is the one HTTP API call shape: {"method": "…", "args": […]}.
type request struct {
	Method string `json:"method"`
	Args   []any  `json:"args"`
}

// errorBody matches an app-level failure: HTTP 500 with {"error": "…"}.
type errorBody struct {
	Error string `json:"error"`
}

// call issues one POST and decodes out (which may be JSON null).
func (c *apiClient) call(ctx context.Context, method string, args []any, out any) error {
	body, err := json.Marshal(request{Method: method, Args: args})
	if err != nil {
		return fmt.Errorf("logseq api: %s: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("logseq api: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.hc.Do(req)
	if err != nil {
		cause := errorCause(err)
		if tlsHint(cause) != "" {
			return fmt.Errorf("logseq api: %s: %w (%s)", method, cause, tlsHint(cause))
		}
		return fmt.Errorf("logseq api: %s: %w", method, cause)
	}
	defer func() { _ = resp.Body.Close() }() // a response body's close error is never actionable

	data, err := io.ReadAll(io.LimitReader(resp.Body, readLimit))
	if err != nil {
		return fmt.Errorf("logseq api: %s: %w", method, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("logseq api: %s: HTTP %s: %s", method, resp.Status, snippet(data))
	}

	var apiErr errorBody
	if err := json.Unmarshal(data, &apiErr); err == nil && apiErr.Error != "" {
		return fmt.Errorf("logseq api: %s: %s", method, apiErr.Error)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("logseq api: %s: decode: %w (body: %s)", method, err, snippet(data))
	}
	return nil
}

// readLimit caps one response: page trees are the big payloads and pages of
// notes stay well under it; anything bigger is not a page this app wants us
// to have.
const readLimit = 64 << 20

// errorCause shortens the transport noise: a *url.Error wraps the real
// cause (dial refused, timeout), so it is unwrapped for the message.
func errorCause(err error) error {
	var uErr *url.Error
	if errors.As(err, &uErr) {
		return uErr.Err
	}
	return err
}

// tlsHint names the fix when the handshake failed on trust: the stock app
// serves plain HTTP, so an https api_url against a self-signed or
// private-CA endpoint fails here with x509 unknown authority. The hint
// points at the scheme first, then the two ways to trust the endpoint.
func tlsHint(err error) string {
	var unknown x509.UnknownAuthorityError
	if errors.As(err, &unknown) {
		return "check logseq.api_url scheme (Logseq defaults to http://127.0.0.1:12315/api); " +
			"for self-signed TLS set logseq.insecure: true, or install the CA in the system store"
	}
	var invalid x509.CertificateInvalidError
	if errors.As(err, &invalid) {
		return "check logseq.api_url scheme (Logseq defaults to http://127.0.0.1:12315/api); " +
			"for self-signed TLS set logseq.insecure: true, or install the CA in the system store"
	}
	var verify *tls.CertificateVerificationError
	if errors.As(err, &verify) {
		return "check logseq.api_url scheme (Logseq defaults to http://127.0.0.1:12315/api); " +
			"for self-signed TLS set logseq.insecure: true, or install the CA in the system store"
	}
	return ""
}

// snippet renders a short body prefix for error messages.
func snippet(data []byte) string {
	const at = 300
	s := string(data)
	if len(s) > at {
		s = s[:at] + "…"
	}
	return strings.TrimSpace(s)
}

// Pages lists every page by calling editor_get_all_pages. The response rows
// carry name plus metadata; decoding keeps only what the adapter returns.
func (c *apiClient) Pages(ctx context.Context) ([]*Page, error) {
	var pages []*Page
	if err := c.call(ctx, "logseq.Editor.get_all_pages", []any{}, &pages); err != nil {
		return nil, err
	}
	return pages, nil
}

// Page returns one page's properties by calling editor_get_page, nil when
// the page does not exist (the app answers JSON null).
func (c *apiClient) Page(ctx context.Context, name string) (*Page, error) {
	var p *Page
	if err := c.call(ctx, "logseq.Editor.get_page", []any{name}, &p); err != nil {
		return nil, err
	}
	return p, nil
}

// Tree returns a page's block tree by calling editor_get_page_blocks_tree.
// The app answers null for a missing page and [] for an empty one; the
// adapter returns nil for both so callers never distinguish.
func (c *apiClient) Tree(ctx context.Context, name string) ([]*Block, error) {
	var tree []*Block
	if err := c.call(ctx, "logseq.Editor.get_page_blocks_tree", []any{name}, &tree); err != nil {
		return nil, err
	}
	if len(tree) == 0 {
		return nil, nil
	}
	canonTree(tree)
	return tree, nil
}

// canonTree unifies the payload's absent values: empty property maps and
// empty child lists become nil, so `{}` and `[]` rows decode to the same
// shape a null row does and callers never tell them apart.
func canonTree(tree []*Block) {
	for _, b := range tree {
		if len(b.Properties) == 0 {
			b.Properties = nil
		}
		if len(b.PropsText) == 0 {
			b.PropsText = nil
		}
		canonTree(b.Children)
		if len(b.Children) == 0 {
			b.Children = nil
		}
	}
}

// CreatePage creates a page with no blocks yet (logseq.Editor.create_page).
func (c *apiClient) CreatePage(ctx context.Context, name string) (*Page, error) {
	var p *Page
	if err := c.call(ctx, "logseq.Editor.create_page",
		[]any{name, nil, map[string]any{"redirect": false, "format": "markdown"}}, &p); err != nil {
		return nil, err
	}
	return p, nil
}

// AppendBlock appends one top-level block to a page
// (logseq.Editor.append_block_in_page) and returns it.
func (c *apiClient) AppendBlock(ctx context.Context, page, content string) (*Block, error) {
	var b *Block
	if err := c.call(ctx, "logseq.Editor.append_block_in_page", []any{page, content, nil}, &b); err != nil {
		return nil, err
	}
	return b, nil
}

// UpdateBlock replaces a block's content in place
// (logseq.Editor.update_block); the app answers an empty object.
func (c *apiClient) UpdateBlock(ctx context.Context, uuid, content string) error {
	return c.call(ctx, "logseq.Editor.update_block", []any{uuid, content, nil}, nil)
}

// InsertBlock inserts a block as the last child of its parent
// (logseq.Editor.insert_block) and returns it.
func (c *apiClient) InsertBlock(ctx context.Context, parentUUID, content string) (*Block, error) {
	var b *Block
	if err := c.call(ctx, "logseq.Editor.insert_block",
		[]any{parentUUID, content, map[string]any{"sibling": false}}, &b); err != nil {
		return nil, err
	}
	return b, nil
}

// SetBlockProperty sets one block property, e.g. default:: true
// (logseq.Editor.upsert_block_property).
func (c *apiClient) SetBlockProperty(ctx context.Context, uuid, key string, value any) error {
	return c.call(ctx, "logseq.Editor.upsert_block_property", []any{uuid, key, value, nil}, nil)
}
