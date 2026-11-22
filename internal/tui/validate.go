package tui

import (
	"fmt"
	"maps"
	"strings"

	"github.com/aries1980/lodo/internal/graph"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/resolve"
	"github.com/aries1980/lodo/internal/set"
)

func stripHintLines(content string) string {
	var kept []string
	for line := range strings.SplitSeq(content, "\n") {
		if strings.HasPrefix(line, "# lodo-tui:") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func isPropLine(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	idx := strings.Index(trimmed, "::")
	if idx <= 0 || idx > 64 {
		return false
	}
	key := trimmed[:idx]
	after := trimmed[idx+2:]
	if len(key) > 0 && strings.ContainsAny(key, ":\"'()") {
		return false
	}
	return after == "" || strings.HasPrefix(after, " ") || strings.HasPrefix(after, "\t")
}

func parsePropLines(content string) map[string]string {
	out := map[string]string{}
	for line := range strings.SplitSeq(content, "\n") {
		if !isPropLine(line) {
			continue
		}
		trimmed := strings.TrimLeft(line, " \t")
		idx := strings.Index(trimmed, "::")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		val := strings.TrimSpace(trimmed[idx+2:])
		if key == "" {
			continue
		}
		out[key] = val
	}
	return out
}

func inferMarker(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if isPropLine(t) {
			continue
		}
		if strings.HasPrefix(t, "# lodo-tui:") {
			continue
		}
		first := strings.Fields(t)[0]
		switch strings.ToUpper(first) {
		case "TODO", "DOING", "DONE", "CANCELLED":
			return strings.ToUpper(first)
		default:
			return ""
		}
	}
	return ""
}

func deepCopyBlock(b *graph.Block) *graph.Block {
	if b == nil {
		return nil
	}
	out := &graph.Block{
		UUID:     b.UUID,
		Content:  b.Content,
		Marker:   b.Marker,
		Children: nil,
	}
	if b.Properties != nil {
		out.Properties = maps.Clone(b.Properties)
	}
	if b.PropsText != nil {
		out.PropsText = maps.Clone(b.PropsText)
	}
	if b.Children != nil {
		out.Children = make([]*graph.Block, 0, len(b.Children))
		for _, c := range b.Children {
			out.Children = append(out.Children, deepCopyBlock(c))
		}
	}
	return out
}

func deepCopyTrees(trees map[string][]*graph.Block) map[string][]*graph.Block {
	out := make(map[string][]*graph.Block, len(trees))
	for k, v := range trees {
		cp := make([]*graph.Block, 0, len(v))
		for _, b := range v {
			cp = append(cp, deepCopyBlock(b))
		}
		out[k] = cp
	}
	return out
}

func findBlock(trees map[string][]*graph.Block, uuid string) *graph.Block {
	var found *graph.Block
	for _, tree := range trees {
		_ = graph.Walk(tree, func(b *graph.Block) error {
			if b.UUID == uuid {
				found = b
			}
			return nil
		})
		if found != nil {
			return found
		}
	}
	return nil
}

func ValidateEdited(data *Data, uuid, edited string) (model.Task, []resolve.Issue, bool) {
	clean := stripHintLines(edited)
	graph := data.GraphOf[uuid]
	input, ok := data.Inputs[graph]
	if !ok {
		return model.Task{}, nil, false
	}
	input.Trees = deepCopyTrees(input.Trees)
	target := findBlock(input.Trees, uuid)
	if target == nil {
		return model.Task{}, nil, false
	}
	target.Content = clean
	target.Marker = inferMarker(clean)
	if target.Marker != string(model.MarkerTODO) && target.Marker != string(model.MarkerDOING) {
		return invalidEdit(uuid, "keep the block marked TODO or DOING")
	}
	target.Properties = nil
	props := parsePropLines(clean)
	if len(props) == 0 {
		target.PropsText = nil
	} else {
		target.PropsText = make(map[string]any, len(props))
		for k, v := range props {
			target.PropsText[k] = v
		}
	}
	res, err := resolve.Resolve(input)
	if err != nil {
		return invalidEdit(uuid, err.Error())
	}
	var relevant []resolve.Issue
	for _, is := range res.Issues {
		if !strings.Contains(is.Context, uuid) {
			continue
		}
		if is.Kind == resolve.IssueUnknownProperty {
			continue
		}
		relevant = append(relevant, is)
	}
	if len(relevant) > 0 {
		return model.Task{}, relevant, false
	}
	for _, t := range res.Tasks {
		if t.ID == uuid {
			return t, nil, true
		}
	}
	return invalidEdit(uuid, "the edited block must remain a Task with an explicit positive duration")
}

func invalidEdit(uuid, detail string) (model.Task, []resolve.Issue, bool) {
	return model.Task{}, []resolve.Issue{{Kind: resolve.IssueBadProperty, Context: fmt.Sprintf("block %s", uuid), Detail: detail}}, false
}

func PatchTimeMap(content, slug string, clear bool) string {
	lines := strings.Split(stripHintLines(content), "\n")
	patched := false
	var out []string
	for _, line := range lines {
		if !isPropLine(line) {
			out = append(out, line)
			continue
		}
		trimmed := strings.TrimLeft(line, " \t")
		idx := strings.Index(trimmed, "::")
		if idx <= 0 {
			out = append(out, line)
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		if !strings.EqualFold(key, "time-map") {
			out = append(out, line)
			continue
		}
		if patched {
			continue
		}
		patched = true
		if clear {
			continue
		}
		lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		out = append(out, lead+"time-map:: "+slug)
	}
	if !patched && !clear {
		out = append(out, "time-map:: "+slug)
	}
	return strings.Join(out, "\n")
}

func TimeMapSlugs(d *Data) []string {
	seen := set.New[string]()
	var out []string
	for _, m := range d.TimeMaps {
		if m.Slug == "" || !seen.Add(m.Slug) {
			continue
		}
		out = append(out, m.Slug)
	}
	return out
}
