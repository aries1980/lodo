// Package workspace is the keep-workspace backend: the official Google
// Keep REST API for paid Google Workspace accounts (keep-integration.md,
// ADR 0026). It implements graph.Client over the note/item mapping shared
// in the parent keep package: one note is one page, one checklist item is
// one block (unchecked TODO, checked DONE), the Time Maps note is a text
// note parsed back to a tree.
package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/graph"
	"github.com/aries1980/lodo/internal/jsonx"
	"github.com/aries1980/lodo/internal/keep"
	"github.com/aries1980/lodo/internal/norm"
)

// KeepBase is the official Keep API root. Production points at Google;
// tests point it at an httptest fake.
const KeepBase = "https://keep.googleapis.com"

// DefaultTimeout bounds one API call, like the other adapters: a call
// that sits for half a minute has stopped answering.
const DefaultTimeout = 30 * time.Second

// readLimit caps one response: note lists are the big payload and pages
// of notes stay well under it.
const readLimit = 64 << 20

// Client is the Workspace adapter. The caller supplies an
// OAuth-authenticated *http.Client (same loopback grant as Calendar,
// plus the https://www.googleapis.com/auth/keep scope); hc nil means the
// default transport with DefaultTimeout, which only works against fakes.
type Client struct {
	hc     *http.Client
	base   string
	filter labelFilter
}

// labelFilter selects which notes belong to the source.
type labelFilter struct {
	// label is the Keep label name; empty means all untrashed notes.
	label string
}

// NewClient starts one adapter over the official Keep API. base ""
// points at Google (KeepBase); label "" selects every untrashed,
// unarchived note, otherwise only notes carrying that label. hc should
// carry the OAuth grant; nil is only useful against test fakes.
func NewClient(hc *http.Client, base, label string) (*Client, error) {
	if strings.TrimSpace(base) == "" {
		base = KeepBase
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("keep client: base %q: must be an absolute http(s) URL", base)
	}
	if hc == nil {
		hc = &http.Client{Timeout: DefaultTimeout}
	}
	return &Client{hc: hc, base: strings.TrimRight(base, "/"), filter: labelFilter{label: strings.TrimSpace(label)}}, nil
}

// wireText is a text note body.
type wireText struct {
	Text string `json:"text,omitempty"`
}

// wireItem is one checklist entry on the wire.
type wireItem struct {
	Name    string    `json:"name,omitempty"`
	Text    *wireText `json:"text,omitempty"`
	Checked bool      `json:"checked,omitempty"`
}

// wireList is a list note body.
type wireList struct {
	ListItems []wireItem `json:"listItems,omitempty"`
}

// wireBody is a note body: exactly one of text or list is present.
type wireBody struct {
	Text *wireText `json:"text,omitempty"`
	List *wireList `json:"list,omitempty"`
}

// wireNote is the note shape this adapter reads and writes: title plus
// the body union, the state flags, and labels decoded leniently (ids or
// name objects) so label filtering survives API shape skew.
type wireNote struct {
	Name     string    `json:"name,omitempty"`
	Title    string    `json:"title,omitempty"`
	Body     *wireBody `json:"body,omitempty"`
	Trashed  bool      `json:"trashed,omitempty"`
	Archived bool      `json:"archived,omitempty"`
	LabelIDs []string  `json:"labelIds,omitempty"`
	Labels   []any     `json:"labels,omitempty"`
}

// wireListOut is a notes.list response page.
type wireListOut struct {
	Notes         []wireNote `json:"notes,omitempty"`
	NextPageToken string     `json:"nextPageToken,omitempty"`
}

// wireLabelsOut is a labels.list response.
type wireLabelsOut struct {
	Labels []struct {
		Name  string `json:"name,omitempty"`
		Title string `json:"title,omitempty"`
	} `json:"labels,omitempty"`
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// toNote converts the wire shape to the protocol-neutral note.
func toNote(w wireNote) keep.Note {
	n := keep.Note{ID: w.Name, Title: w.Title}
	if w.Body != nil && w.Body.List != nil {
		n.IsList = true
		for _, it := range w.Body.List.ListItems {
			text := ""
			if it.Text != nil {
				text = it.Text.Text
			}
			n.Items = append(n.Items, keep.Item{ID: it.Name, Text: text, Checked: it.Checked})
		}
	} else if w.Body != nil && w.Body.Text != nil {
		n.Text = w.Body.Text.Text
	}
	for _, l := range w.Labels {
		switch t := l.(type) {
		case string:
			n.Labels = append(n.Labels, t)
		case map[string]any:
			if s, ok := t["name"].(string); ok && s != "" {
				n.Labels = append(n.Labels, s)
			} else if s, ok := t["title"].(string); ok && s != "" {
				n.Labels = append(n.Labels, s)
			}
		}
	}
	n.Labels = append(n.Labels, w.LabelIDs...)
	return n
}

// fromNote converts back, preserving ids so updates address items.
func fromNote(n keep.Note) wireNote {
	w := wireNote{Name: n.ID, Title: n.Title}
	if n.IsList {
		body := &wireBody{List: &wireList{}}
		for _, it := range n.Items {
			body.List.ListItems = append(body.List.ListItems, wireItem{Name: it.ID, Text: &wireText{Text: it.Text}, Checked: it.Checked})
		}
		w.Body = body
	} else {
		w.Body = &wireBody{Text: &wireText{Text: n.Text}}
	}
	return w
}

// call issues one JSON request and decodes out (which may be nil).
func (c *Client) call(ctx context.Context, method, path string, query url.Values, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := jsonx.Marshal(in)
		if err != nil {
			return fmt.Errorf("keep api: %s %s: %w", method, path, err)
		}
		body = bytes.NewReader(data)
	}
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return fmt.Errorf("keep api: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("keep api: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, readLimit))
	if err != nil {
		return fmt.Errorf("keep api: %s %s: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &keep.APIError{StatusCode: resp.StatusCode, Op: method + " " + path, Status: resp.Status, Message: snippet(data)}
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := jsonx.Unmarshal(data, out); err != nil {
		return fmt.Errorf("keep api: %s %s: decode: %w (body: %s)", method, path, err, snippet(data))
	}
	return nil
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

// labelID resolves the configured label to its id via labels.list, or ""
// when no label filter is configured.
func (c *Client) labelID(ctx context.Context) (string, error) {
	if c.filter.label == "" {
		return "", nil
	}
	var out wireLabelsOut
	if err := c.call(ctx, http.MethodGet, "/v1/labels", nil, nil, &out); err != nil {
		// labels.list is best-effort: when the endpoint is absent the
		// name match below still applies to inline label names.
		return "", nil
	}
	want := norm.ID(c.filter.label)
	for _, l := range out.Labels {
		if norm.ID(l.Title) == want || norm.ID(lastSegment(l.Name)) == want || norm.ID(l.Name) == want {
			if l.Name != "" {
				return l.Name, nil
			}
			return l.Title, nil
		}
	}
	return "", nil
}

func lastSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// listNotes returns every visible note in the source: untrashed,
// unarchived, label-filtered client-side.
func (c *Client) listNotes(ctx context.Context) ([]keep.Note, error) {
	var all []wireNote
	pageToken := ""
	for {
		q := url.Values{}
		q.Set("pageSize", "100")
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		var out wireListOut
		if err := c.call(ctx, http.MethodGet, "/v1/notes", q, nil, &out); err != nil {
			return nil, err
		}
		all = append(all, out.Notes...)
		if out.NextPageToken == "" {
			break
		}
		pageToken = out.NextPageToken
	}
	id, _ := c.labelID(ctx)
	var visible []keep.Note
	for _, w := range all {
		if w.Trashed || w.Archived {
			continue
		}
		visible = append(visible, toNote(w))
	}
	return keep.SelectNotes(visible, c.filter.label, id), nil
}

// getNote fetches one note by name.
func (c *Client) getNote(ctx context.Context, name string) (keep.Note, error) {
	var w wireNote
	if err := c.call(ctx, http.MethodGet, "/v1/"+name, nil, nil, &w); err != nil {
		return keep.Note{}, err
	}
	return toNote(w), nil
}

// visibleNote fetches one note by id for a page read. A missing note and
// one that left the visible set since Pages listed it (trashed or
// archived) both read as "no page", like a title that matches nothing.
func (c *Client) visibleNote(ctx context.Context, id string) (*wireNote, error) {
	var w wireNote
	if err := c.call(ctx, http.MethodGet, "/v1/"+id, nil, nil, &w); err != nil {
		if apiErr, ok := errors.AsType[*keep.APIError](err); ok && apiErr.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	if w.Trashed || w.Archived {
		return nil, nil
	}
	return &w, nil
}

// putNote replaces one note's title and body.
func (c *Client) putNote(ctx context.Context, n keep.Note, mask string) (keep.Note, error) {
	q := url.Values{}
	if mask != "" {
		q.Set("updateMask", mask)
	}
	var w wireNote
	if err := c.call(ctx, http.MethodPatch, "/v1/"+n.ID, q, fromNote(n), &w); err != nil {
		return keep.Note{}, err
	}
	if w.Name == "" {
		return n, nil
	}
	return toNote(w), nil
}

// Ping probes whether the API answers at all.
func (c *Client) Ping(ctx context.Context) error {
	q := url.Values{}
	q.Set("pageSize", "1")
	var out wireListOut
	if err := c.call(ctx, http.MethodGet, "/v1/notes", q, nil, &out); err != nil {
		return err
	}
	return nil
}

// Pages lists every visible note as a page.
func (c *Client) Pages(ctx context.Context) ([]*graph.Page, error) {
	notes, err := c.listNotes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*graph.Page, 0, len(notes))
	for _, n := range notes {
		out = append(out, keep.NoteToPage(n))
	}
	return out, nil
}

// Page returns one note's page, or nil when no note carries the title.
// A known note id (uuid) fetches the note directly; an empty uuid falls
// back to a title lookup over the full note list.
func (c *Client) Page(ctx context.Context, uuid, name string) (*graph.Page, error) {
	if uuid != "" {
		w, err := c.visibleNote(ctx, uuid)
		if err != nil || w == nil {
			return nil, err
		}
		return keep.NoteToPage(toNote(*w)), nil
	}
	notes, err := c.listNotes(ctx)
	if err != nil {
		return nil, err
	}
	n, err := keep.FindByTitle(notes, name)
	if err != nil {
		if err == keep.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return keep.NoteToPage(n), nil
}

// Tree returns a note's block tree; nil when the note is missing or
// holds no blocks. A known note id (uuid) fetches the note directly; an
// empty uuid falls back to a title lookup over the full note list.
func (c *Client) Tree(ctx context.Context, uuid, name string) ([]*graph.Block, error) {
	if uuid != "" {
		w, err := c.visibleNote(ctx, uuid)
		if err != nil || w == nil {
			return nil, err
		}
		tree := keep.NoteToTree(toNote(*w))
		if len(tree) == 0 {
			return nil, nil
		}
		return tree, nil
	}
	notes, err := c.listNotes(ctx)
	if err != nil {
		return nil, err
	}
	n, err := keep.FindByTitle(notes, name)
	if err != nil {
		if err == keep.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	full, err := c.getNote(ctx, n.ID)
	if err != nil {
		return nil, err
	}
	tree := keep.NoteToTree(full)
	if len(tree) == 0 {
		return nil, nil
	}
	return tree, nil
}

// CreatePage creates a text note titled name. Only the Time Maps
// bootstrap calls this, and the Time Maps note is always a text note.
func (c *Client) CreatePage(ctx context.Context, name string) (*graph.Page, error) {
	var w wireNote
	in := wireNote{Title: name, Body: &wireBody{Text: &wireText{}}}
	if err := c.call(ctx, http.MethodPost, "/v1/notes", nil, in, &w); err != nil {
		return nil, err
	}
	n := toNote(w)
	if n.ID == "" {
		n.ID = w.Name
		n.Title = name
	}
	return keep.NoteToPage(n), nil
}

// AppendBlock appends one top-level block: a text line on the Time Maps
// note, a checklist item on task notes.
func (c *Client) AppendBlock(ctx context.Context, page, content string) (*graph.Block, error) {
	notes, err := c.listNotes(ctx)
	if err != nil {
		return nil, err
	}
	n, err := keep.FindByTitle(notes, page)
	if err != nil {
		return nil, err
	}
	full, err := c.getNote(ctx, n.ID)
	if err != nil {
		return nil, err
	}
	if !full.IsList {
		tree := keep.ParseTextNote(full.ID, full.Text)
		b := &graph.Block{Content: content}
		if props := keep.ParsePropLines(content); len(props) > 0 {
			b.PropsText = props
		}
		tree = append(tree, b)
		full.Text = keep.RenderTextNote(tree)
		if _, err := c.putNote(ctx, full, "body.text.text"); err != nil {
			return nil, err
		}
		return freshness(full, len(tree)-1)
	}
	id := newItemID(full)
	full.Items = append(full.Items, keep.Item{ID: id, Text: content, Checked: keep.CheckedFor(content)})
	updated, err := c.putNote(ctx, full, "body.list.listItems")
	if err != nil {
		return nil, err
	}
	return keep.ItemToBlock(updated.ID, keep.Item{ID: id, Text: content, Checked: keep.CheckedFor(content)}), nil
}

// freshness returns the block just appended to a text note tree.
func freshness(n keep.Note, index int) (*graph.Block, error) {
	tree := keep.ParseTextNote(n.ID, n.Text)
	if index < 0 || index >= len(tree) {
		return nil, fmt.Errorf("keep: appended block is not in the note tree")
	}
	return tree[index], nil
}

// newItemID synthesizes a client-side item id for an appended item; the
// API answers with the canonical one, which putNote reconciles.
func newItemID(n keep.Note) string {
	return "items/" + strconv.Itoa(len(n.Items)+1)
}

// UpdateBlock replaces a block's content in place, syncing the checklist
// state from the text marker on list notes.
func (c *Client) UpdateBlock(ctx context.Context, uuid, content string) error {
	if noteID, idx, ok := keep.SplitTextUUID(uuid); ok {
		return c.updateTextBlock(ctx, noteID, idx, func(b *graph.Block) {
			b.Content = content
			b.PropsText = keep.ParsePropLines(content)
		})
	}
	noteID, itemID, ok := keep.SplitItemID(uuid)
	if !ok {
		return fmt.Errorf("keep: block %q: malformed block id", uuid)
	}
	n, err := c.getNote(ctx, noteID)
	if err != nil {
		return err
	}
	if !n.IsList {
		return fmt.Errorf("keep: block %q: not a checklist item", uuid)
	}
	at := -1
	for i := range n.Items {
		if n.Items[i].ID == itemID || lastSegment(n.Items[i].ID) == lastSegment(itemID) {
			at = i
			break
		}
	}
	if at < 0 {
		return fmt.Errorf("keep: block %q: no such item", uuid)
	}
	n.Items[at].Text = content
	n.Items[at].Checked = keep.CheckedFor(content)
	_, err = c.putNote(ctx, n, "body.list.listItems")
	return err
}

// InsertBlock inserts a block as the last child of its parent. List
// notes have no nesting: the block lands as a top-level item instead
// (keep-integration.md). Text notes (Time Maps) nest via indentation.
func (c *Client) InsertBlock(ctx context.Context, parentUUID, content string) (*graph.Block, error) {
	if noteID, idx, ok := keep.SplitTextUUID(parentUUID); ok {
		var child *graph.Block
		err := c.updateTextBlock(ctx, noteID, idx, func(b *graph.Block) {
			child = &graph.Block{Content: content}
			if props := keep.ParsePropLines(content); len(props) > 0 {
				child.PropsText = props
			}
			b.Children = append(b.Children, child)
		})
		if err != nil {
			return nil, err
		}
		return child, nil
	}
	noteID, _, ok := keep.SplitItemID(parentUUID)
	if !ok {
		return nil, fmt.Errorf("keep: block %q: malformed block id", parentUUID)
	}
	n, err := c.getNote(ctx, noteID)
	if err != nil {
		return nil, err
	}
	if !n.IsList {
		return nil, fmt.Errorf("keep: block %q: not a checklist item", parentUUID)
	}
	return c.AppendBlock(ctx, n.Title, content)
}

// SetBlockProperty sets one block property: a key:: line on text notes,
// a key:: line inside the item text on list notes.
func (c *Client) SetBlockProperty(ctx context.Context, uuid, key string, value any) error {
	if noteID, idx, ok := keep.SplitTextUUID(uuid); ok {
		return c.updateTextBlock(ctx, noteID, idx, func(b *graph.Block) {
			if b.PropsText == nil {
				b.PropsText = map[string]any{}
			}
			if s, ok := propText(value); ok {
				b.PropsText[strings.TrimSpace(key)] = s
			}
			b.Content = keep.AppendPropLine(b.Content, strings.TrimSpace(key), value)
		})
	}
	noteID, itemID, ok := keep.SplitItemID(uuid)
	if !ok {
		return fmt.Errorf("keep: block %q: malformed block id", uuid)
	}
	n, err := c.getNote(ctx, noteID)
	if err != nil {
		return err
	}
	at := -1
	for i := range n.Items {
		if n.Items[i].ID == itemID || lastSegment(n.Items[i].ID) == lastSegment(itemID) {
			at = i
			break
		}
	}
	if at < 0 {
		return fmt.Errorf("keep: block %q: no such item", uuid)
	}
	n.Items[at].Text = keep.SetItemProp(n.Items[at].Text, key, value)
	_, err = c.putNote(ctx, n, "body.list.listItems")
	return err
}

// propText renders a property value the way the source would carry it.
func propText(value any) (string, bool) {
	switch t := value.(type) {
	case nil:
		return "", false
	case string:
		return t, true
	case bool:
		if t {
			return "true", true
		}
		return "false", true
	default:
		return "", false
	}
}

// updateTextBlock reads a text note, applies mutate to its idx-th
// preorder block, and writes the note back.
func (c *Client) updateTextBlock(ctx context.Context, noteID string, idx int, mutate func(*graph.Block)) error {
	n, err := c.getNote(ctx, noteID)
	if err != nil {
		return err
	}
	if n.IsList {
		return fmt.Errorf("keep: note %s is a checklist; want the outline note", noteID)
	}
	tree := keep.ParseTextNote(n.ID, n.Text)
	var flat []*graph.Block
	var walk func(bs []*graph.Block)
	walk = func(bs []*graph.Block) {
		for _, b := range bs {
			flat = append(flat, b)
			walk(b.Children)
		}
	}
	walk(tree)
	if idx < 0 || idx >= len(flat) {
		return fmt.Errorf("keep: block %q: no such block", keep.TextUUID(noteID, idx))
	}
	mutate(flat[idx])
	n.Text = keep.RenderTextNote(tree)
	_, err = c.putNote(ctx, n, "body.text.text")
	return err
}
