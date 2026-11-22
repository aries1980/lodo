// Package keep is the shared Google Keep mapping behind the
// backend-neutral seam in internal/graph (ADR 0026). It holds the
// protocol-neutral model (Note, Item), the checklist-item-per-task mapping
// and the Time Maps outline engine. The two transports live beneath it
// and share everything here:
//
//   - internal/keep/workspace (keep-workspace): the official Keep REST API
//     for paid Google Workspace accounts, OAuth scope
//     https://www.googleapis.com/auth/keep.
//   - internal/keep/personal (keep-personal): the token-authenticated sync
//     transport for free personal @gmail.com accounts, which the official
//     API rejects with 400 invalid_scope. Experimental: the sync protocol
//     is reverse-engineered and may break on Google-side changes.
//
// Mapping (checklist-item-per-task): one Keep note is one graph.Page, one
// checklist item is one graph.Block (unchecked TODO, checked DONE).
// Backends without native hierarchy return flat trees: no containers, no
// sequential chains, page-level inheritance only. The one exception is
// the Time Maps note, which is a text note whose indented outline parses
// back into a tree, so Time Map windows keep working.
package keep

import (
	"strings"
)

// Backend names the Keep adapters for diagnostics, config validation and
// run.Source.Backend. The split is by account type, because the two access
// paths are entirely different protocols: keep-workspace (official REST,
// paid Workspace accounts, internal/keep/workspace) and keep-personal
// (token sync transport, free personal accounts, internal/keep/personal).
const (
	// BackendWorkspace is the official Keep REST API (Workspace accounts).
	BackendWorkspace = "keep-workspace"
	// BackendPersonal is the personal-account sync transport. Experimental.
	BackendPersonal = "keep-personal"
)

// ValidBackend reports whether name selects a Keep adapter.
func ValidBackend(name string) bool {
	return name == BackendWorkspace || name == BackendPersonal
}

// Environment variables carrying the personal-account sync token. The
// per-source form mirrors the Logseq convention (TokenForGraph):
// work -> LODO_KEEP_TOKEN_WORK.
const (
	// EnvKeepToken carries the personal sync token (or the per-source
	// default when no source-specific variable is set).
	EnvKeepToken = "LODO_KEEP_TOKEN"
	// EnvKeepSyncURL carries the personal sync endpoint root.
	EnvKeepSyncURL = "LODO_KEEP_SYNC_URL"
)

// Note is the protocol-neutral shape both transports map from: one Keep
// note with either checklist items (task notes) or outline text (the Time
// Maps note).
type Note struct {
	ID     string
	Title  string
	Text   string
	Items  []Item
	IsList bool
	Labels []string
}

// Item is one checklist entry of a list note.
type Item struct {
	ID      string
	Text    string
	Checked bool
}

// ItemID composes a session-stable block UUID for a checklist item. The
// note id scopes it; the item id is Google-issued and stable across runs.
// The separator is '#': Keep resource names never contain it, while both
// note and item ids contain '/' (notes/N, notes/N/listItems/M).
func ItemID(noteID, itemID string) string {
	return noteID + "#" + itemID
}

// SplitItemID reverses ItemID into its note and item parts.
func SplitItemID(uuid string) (noteID, itemID string, ok bool) {
	i := strings.LastIndexByte(uuid, '#')
	if i <= 0 || i == len(uuid)-1 {
		return "", "", false
	}
	return uuid[:i], uuid[i+1:], true
}

// MarkerFor maps a checklist state to a task marker. Keep has no native
// DOING or CANCELLED: unchecked is TODO, checked is DONE. A DOING or
// CANCELLED text marker survives in the content but the checked state
// decides scheduling candidacy, like the DONE/CANCELLED gate.
func MarkerFor(checked bool) string {
	if checked {
		return "DONE"
	}
	return "TODO"
}

// CheckedFor maps edited block text back to a checklist state: a leading
// DONE/CANCELLED marker checks the item, anything else unchecks it. The
// text itself is kept verbatim so the marker stays visible in Keep.
func CheckedFor(content string) bool {
	switch MarkerForText(content) {
	case "DONE", "CANCELLED":
		return true
	}
	return false
}

// MarkerForText reads the leading task marker of raw block text, or ""
// when the first content line carries none.
func MarkerForText(content string) string {
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || isPropLine(t) || isHintLine(t) {
			continue
		}
		first := strings.ToUpper(firstField(t))
		switch first {
		case "TODO", "DOING", "DONE", "CANCELLED":
			return first
		default:
			return ""
		}
	}
	return ""
}
