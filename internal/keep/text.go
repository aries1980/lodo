package keep

import (
	"slices"
	"strconv"
	"strings"

	"github.com/aries1980/lodo/internal/graph"
)

// The Time Maps note is a text note whose outline this engine parses back
// into a block tree, so Time Map windows keep working on a backend with
// no native hierarchy. Indentation is two spaces per level; prop lines
// (key:: value) attach to the current block and are also parsed into
// PropsText so the resolver reads them unchanged.

func firstField(s string) string {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}

func isHintLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "# lodo-tui:")
}

// isPropLine mirrors the resolver's property-line rule: a short key plus
// "::" with whitespace (or nothing) after the value separator.
func isPropLine(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	idx := strings.Index(trimmed, "::")
	if idx <= 0 || idx > 64 {
		return false
	}
	key := trimmed[:idx]
	after := trimmed[idx+2:]
	if strings.ContainsAny(key, ":\"'()") {
		return false
	}
	return after == "" || strings.HasPrefix(after, " ") || strings.HasPrefix(after, "\t")
}

// propKeyValue splits a property line into its key and value text.
func propKeyValue(line string) (key, value string) {
	trimmed := strings.TrimLeft(line, " \t")
	idx := strings.Index(trimmed, "::")
	if idx <= 0 {
		return "", ""
	}
	return strings.TrimSpace(trimmed[:idx]), strings.TrimSpace(trimmed[idx+2:])
}

// indentOf counts leading spaces/tabs of a line.
func indentOf(line string) int {
	n := 0
	for _, r := range line {
		if r != ' ' && r != '\t' {
			break
		}
		n++
	}
	return n
}

// TextUUID composes a session-stable block UUID for a text-note block:
// the note id plus its preorder index at read time. Map blocks need no
// cross-run identity (only Tasks do), so read-time indexes suffice; every
// mutating op re-reads before writing.
func TextUUID(noteID string, index int) string {
	return noteID + "#L" + strconv.Itoa(index)
}

// SplitTextUUID reverses TextUUID into its note id and preorder index.
func SplitTextUUID(uuid string) (noteID string, index int, ok bool) {
	i := strings.LastIndex(uuid, "#L")
	if i < 0 {
		return "", 0, false
	}
	n := 0
	digits := uuid[i+2:]
	if digits == "" {
		return "", 0, false
	}
	for _, ch := range digits {
		if ch < '0' || ch > '9' {
			return "", 0, false
		}
		n = n*10 + int(ch-'0')
	}
	return uuid[:i], n, true
}

// ParseTextNote parses outline text into a block tree. Top-level
// non-prop lines start blocks; deeper-indented non-prop lines nest under
// the nearest shallower block; prop lines attach to the current block.
func ParseTextNote(noteID, text string) []*graph.Block {
	var roots []*graph.Block
	type frame struct {
		block  *graph.Block
		indent int
	}
	var stack []frame
	index := 0
	attach := func(b *graph.Block) {
		b.UUID = TextUUID(noteID, index)
		index++
	}
	for _, raw := range strings.Split(text, "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		indent := indentOf(raw)
		if isPropLine(raw) {
			if len(stack) == 0 {
				continue // a property with no block is not representable; skip
			}
			top := stack[len(stack)-1].block
			k, v := propKeyValue(raw)
			if k == "" {
				continue
			}
			if top.PropsText == nil {
				top.PropsText = map[string]any{}
			}
			top.PropsText[k] = v
			top.Content += "\n" + strings.TrimSpace(raw)
			continue
		}
		b := &graph.Block{Content: strings.TrimSpace(raw)}
		attach(b)
		for len(stack) > 0 && indent <= stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			roots = append(roots, b)
		} else {
			parent := stack[len(stack)-1].block
			parent.Children = append(parent.Children, b)
		}
		stack = append(stack, frame{block: b, indent: indent})
	}
	if len(roots) == 0 {
		return nil
	}
	return roots
}

// RenderTextNote renders a block tree back to outline text: one line per
// block header plus its property lines, children indented two spaces.
func RenderTextNote(blocks []*graph.Block) string {
	var out []string
	var walk func(bs []*graph.Block, indent int)
	walk = func(bs []*graph.Block, indent int) {
		for _, b := range bs {
			if b == nil {
				continue
			}
			pad := strings.Repeat(" ", indent)
			header, props := splitContentProps(b.Content)
			if header != "" {
				out = append(out, pad+header)
			}
			for _, p := range props {
				out = append(out, pad+p)
			}
			for _, kv := range sortedProps(b.PropsText) {
				out = append(out, pad+kv[0]+":: "+kv[1])
			}
			walk(b.Children, indent+2)
		}
	}
	walk(blocks, 0)
	return strings.Join(out, "\n")
}

// splitContentProps splits raw block content into its header lines and
// embedded property lines.
func splitContentProps(content string) (header string, props []string) {
	var kept []string
	for _, line := range strings.Split(content, "\n") {
		if isPropLine(line) {
			props = append(props, strings.TrimSpace(line))
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n")), props
}

// sortedProps renders PropsText deterministically for the outline.
func sortedProps(m map[string]any) [][2]string {
	var out [][2]string
	for k, v := range m {
		s, ok := propText(v)
		if !ok {
			continue
		}
		out = append(out, [2]string{k, s})
	}
	slices.SortFunc(out, func(a, b [2]string) int { return strings.Compare(a[0], b[0]) })
	return out
}

// propText renders a property value the way the graph would carry it.
func propText(v any) (string, bool) {
	switch t := v.(type) {
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
