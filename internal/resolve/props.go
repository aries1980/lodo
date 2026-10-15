package resolve

import (
	"strconv"
	"strings"

	"github.com/aries1980/lodo/internal/logseq"
)

// knownKeys are the properties Lodo reads: tasks.md's table, plus default::
// (Time Maps, ADR 0003) and id:: (Logseq's own identity property).
// Anything else is the graph's own annotation: left alone, but named by
// check when it sits on a task-marked block.
var knownKeys = map[string]bool{
	"duration":   true,
	"time-map":   true,
	"priority":   true,
	"deadline":   true,
	"plan-start": true,
	"plan-end":   true,
	"scheduled":  true,
	"default":    true,
	"id":         true,
}

// inheritableKeys are the properties a page may provide as defaults for its
// blocks (logseq-integration.md): duration, deadline and scheduled:: are
// each block's own, so they never inherit.
var inheritableKeys = map[string]bool{
	"time-map":   true,
	"priority":   true,
	"plan-start": true,
	"plan-end":   true,
}

// normKey compares a property key permissively: case-insensitive, trimmed.
func normKey(k string) string {
	return strings.ToLower(strings.TrimSpace(k))
}

// blockProps collects a block's properties into one permissive map: parsed
// values (which carry types) first, raw text values as fallback, both
// under normalised keys.
func blockProps(b *logseq.Block) map[string]any {
	out := make(map[string]any, len(b.Properties)+len(b.PropsText))
	for k, v := range b.PropsText {
		out[normKey(k)] = v
	}
	for k, v := range b.Properties {
		out[normKey(k)] = v
	}
	return out
}

// propsText renders a property value as the string it stands for: scalars
// verbatim, numbers as numbers, booleans as words; lists join with the
// comma-space the graph itself uses for multi-values. Anything nested
// yields ok=false: such a value is malformed where text is expected and
// must be reported, never guessed.
func propsText(v any) (s string, ok bool) {
	switch t := v.(type) {
	case nil:
		return "", false
	case string:
		return t, true
	case bool:
		return strconv.FormatBool(t), true
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10), true
		}
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case []any:
		var parts []string
		for _, item := range t {
			s, ok := propsText(item)
			if !ok {
				return "", false
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, ", "), true
	default:
		return "", false
	}
}

// stripProps removes the property lines that Logseq stores inside the raw
// content (e.g. "id:: 3444…"), leaving the block's own text.
func stripProps(content string) string {
	var b strings.Builder
	for _, line := range strings.Split(content, "\n") {
		if isPropLine(line) {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

// isPropLine reports whether a raw content line is a property line: a short
// key plus a "::" with whitespace before the value. Logbook lines start
// with a colon and stay.
func isPropLine(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	idx := strings.Index(trimmed, "::")
	if idx <= 0 || idx > maxPropKeyLen {
		return false
	}
	key := trimmed[:idx]
	after := trimmed[idx+2:]
	if len(key) > 0 && strings.ContainsAny(key, ":\"'()") {
		return false
	}
	return after == "" || strings.HasPrefix(after, " ") || strings.HasPrefix(after, "\t")
}

// maxPropKeyLen is generous room for a property key.
const maxPropKeyLen = 64
