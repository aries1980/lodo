package keep

import (
	"strings"

	"github.com/aries1980/lodo/internal/graph"
)

// NoteToPage maps one Keep note to a graph page. The title is the page
// name: titles must be unique within a source, like Logseq page names.
func NoteToPage(n Note) *graph.Page {
	return &graph.Page{Name: n.Title, UUID: n.ID}
}

// NoteToTree maps one Keep note to its block tree. List notes (task
// notes) map flat: one block per checklist item, no children — Keep has
// no nesting, so containers, sequential chains and ancestor inheritance
// do not exist here (keep-integration.md). Text notes (the Time Maps
// note) parse back to a tree via the outline engine.
func NoteToTree(n Note) []*graph.Block {
	if !n.IsList {
		return ParseTextNote(n.ID, n.Text)
	}
	var out []*graph.Block
	for _, it := range n.Items {
		out = append(out, ItemToBlock(n.ID, it))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ItemToBlock maps one checklist item to a block: unchecked TODO,
// checked DONE, property lines parsed into PropsText so the resolver
// reads duration::, time-map:: and friends unchanged.
func ItemToBlock(noteID string, it Item) *graph.Block {
	b := &graph.Block{
		UUID:    ItemID(noteID, it.ID),
		Content: it.Text,
		Marker:  MarkerFor(it.Checked),
	}
	if props := ParsePropLines(it.Text); len(props) > 0 {
		b.PropsText = props
	}
	return b
}

// ParsePropLines extracts key:: value lines from raw item text.
func ParsePropLines(content string) map[string]any {
	var out map[string]any
	for _, line := range splitLines(content) {
		if !isPropLine(line) {
			continue
		}
		k, v := propKeyValue(line)
		if k == "" {
			continue
		}
		if out == nil {
			out = map[string]any{}
		}
		out[k] = v
	}
	return out
}

// SetItemProp returns item text with one key:: line added or replaced.
func SetItemProp(text, key string, value any) string {
	v, ok := propText(value)
	if !ok || strings.TrimSpace(key) == "" {
		return text
	}
	want := strings.TrimSpace(key) + ":: " + v
	var out []string
	replaced := false
	for _, line := range splitLines(text) {
		if isPropLine(line) {
			if k, _ := propKeyValue(line); equalFold(k, strings.TrimSpace(key)) && !replaced {
				out = append(out, want)
				replaced = true
				continue
			}
		}
		out = append(out, line)
	}
	if !replaced {
		out = append(out, want)
	}
	return strings.Join(out, "\n")
}

// AppendPropLine adds or replaces one key:: line in raw block content.
// Text notes and list items share the convention.
func AppendPropLine(content, key string, value any) string {
	return SetItemProp(content, key, value)
}

func equalFold(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
