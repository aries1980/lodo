package resolve

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/parse"
)

// tasks resolves candidate Tasks from every page but the Time Maps page: a
// block is a Task iff it is TODO/DOING and carries an explicit duration::
// (ADR 0004); anything else is a note, skipped — and reported when the
// omission would otherwise be silent.
func (r *resolver) resolveTasks() {
	for _, page := range r.sortedPages() {
		if page == nil || page.Properties == nil {
			continue
		}
		for _, key := range []string{"time-map", "priority", "plan-start", "plan-end"} {
			raw, ok := page.Properties[key]
			if !ok {
				continue
			}
			text, okT := propsText(raw)
			if !okT {
				r.issue(IssueBadProperty,
					fmt.Sprintf("page %s", page.Name),
					fmt.Sprintf("%s:: has a malformed value the app parsed as %v; the page-level default is skipped", key, raw))
				continue
			}
			r.pageDefault(page.Name, key, text)
		}
	}

	order := 0 // depth-first, top-to-bottom position among task candidates
	tmIdx := r.timeMapsIndex()
	for _, page := range r.sortedPages() {
		if idx := r.pageIndex(page.Name); idx == tmIdx {
			continue // the Time Maps page defines maps, never Tasks
		}

		if r.treeFor(page.Name) == nil {
			continue
		}
		page := page
		if err := logseq.Walk(r.treeFor(page.Name), func(b *logseq.Block) error {
			switch model.Marker(strings.ToUpper(strings.TrimSpace(b.Marker))) {
			case model.MarkerTODO, model.MarkerDOING:
			default:
				return nil // DONE/CANCELLED are complete; plain blocks are notes
			}

			t, isTask, issue := r.task(page, b)
			if issue != nil {
				r.issue(issue.Kind, issue.Context, issue.Detail)
			}
			if isTask {
				t.OutlineOrder = order
				r.tasks = append(r.tasks, t)
			}
			order++
			return nil
		}); err != nil {
			return // unreachable: the visitor never errors
		}
	}

	for _, t := range r.tasks {
		for _, slug := range t.TimeMaps {
			if !r.mapSlugs[slug] {
				r.issue(IssueUnknownTimeMap, taskContext(t),
					fmt.Sprintf("time-map:: %s references no Time Map defined on %q", slug, r.in.TimeMapsPage))
			}
		}
	}
}

// task resolves one candidate block. A non-nil Issue is what check must
// name; isTask=false means the block is not a Task (no duration::, or a
// malformed one). The Task keeps the inputs that did resolve.
func (r *resolver) task(page *logseq.Page, b *logseq.Block) (model.Task, bool, *Issue) {
	t := model.Task{
		ID:     strings.TrimSpace(b.UUID),
		Text:   taskText(b),
		Marker: model.Marker(strings.ToUpper(strings.TrimSpace(b.Marker))),
	}

	if t.ID == "" {
		return t, false, &Issue{
			Kind:    IssueMissingBlockID,
			Context: fmt.Sprintf("page %s", page.Name),
			Detail:  "the task-marked block has no UUID, so it cannot be tracked across runs",
		}
	}

	// duration: the gate (ADR 0004). Explicit only — there is no default
	// duration; a TODO without duration is a note (tasks.md).
	rawDur, ok := blockProps(b)["duration"]
	if !ok {
		return t, false, &Issue{
			Kind:    IssueMissingDuration,
			Context: r.blockContext(page, b),
			Detail:  fmt.Sprintf("%q has no duration::, so it is not scheduled; a TODO becomes a Task only with an explicit duration (ADR 0004)", t.Text),
		}
	}
	durText, ok := propsText(rawDur)
	if !ok {
		return t, false, &Issue{
			Kind:    IssueBadProperty,
			Context: r.blockContext(page, b),
			Detail:  fmt.Sprintf("duration:: has a malformed value the app parsed as %v; the block is not scheduled", rawDur),
		}
	}
	dur, durErr := parse.ParseDuration(durText)
	if durErr != nil {
		return t, false, &Issue{
			Kind:    IssueBadProperty,
			Context: r.blockContext(page, b),
			Detail:  fmt.Sprintf("duration:: %q is unparseable (%v); the block is not scheduled", durText, durErr),
		}
	}
	t.Duration = dur

	// time-map:: — slugs are case-insensitive, comma-separated, and only
	// ever block-level or page-inherited.
	if text := r.value(page, b, "time-map"); text != "" {
		for _, slug := range splitSlugs(text) {
			if !containsString(t.TimeMaps, slug) {
				t.TimeMaps = append(t.TimeMaps, slug)
			}
		}
	}

	// priority:: — parsed or denied: a bad value drops the property and the
	// Task ranks unprioritised.
	if text := r.value(page, b, "priority"); text != "" {
		p, pErr := parse.ParsePriority(text)
		if pErr != nil {
			r.appendBad(r.blockContext(page, b), "priority", text, pErr)
		} else {
			t.Priority = p
		}
	}

	// deadline:: — a reference due date; it never places anything.
	if text := r.blockValue(page, b, "deadline"); text != "" {
		d, dErr := parseDate(text)
		if dErr != nil {
			r.appendBad(r.blockContext(page, b), "deadline", text, dErr)
		} else {
			t.Deadline = &d
		}
	}

	// plan-start:: / plan-end:: — day-granular bounds, inheritable.
	if text := r.value(page, b, "plan-start"); text != "" {
		d, pErr := parse.ParseCivilDate(text)
		if pErr != nil {
			r.appendBad(r.blockContext(page, b), "plan-start", text, pErr)
		} else {
			t.PlanStart = &d
		}
	}
	if text := r.value(page, b, "plan-end"); text != "" {
		d, pErr := parse.ParseCivilDate(text)
		if pErr != nil {
			r.appendBad(r.blockContext(page, b), "plan-end", text, pErr)
		} else {
			t.PlanEnd = &d
		}
	}

	// scheduled:: — the Pin: an exact date-time, block-level, never
	// inherited (tasks.md). Its authored time belongs to the scheduling
	// timezone's wall clock.
	if text := r.blockValue(page, b, "scheduled"); text != "" {
		stamp, sErr := parse.ParseOrgStamp(text)
		switch {
		case sErr != nil:
			r.appendBad(r.blockContext(page, b), "scheduled", text, sErr)
		case !stamp.HasTime:
			r.appendBad(r.blockContext(page, b), "scheduled", text,
				fmt.Errorf("no time: a Pin is an exact date-time and the day-granular equivalent is plan-start or plan-end"))
		default:
			pin := stamp.Instant(r.in.Location)
			t.Pin = &pin
		}
	}

	// Unknown properties on a task-marked block: never read, but named, so
	// a key that mattered is not silently ignored.
	var unknown []string
	for key, v := range blockProps(b) {
		if !knownKeys[key] {
			unknown = append(unknown, key)
			continue
		}
		_ = v
	}
	sort.Strings(unknown)
	for _, key := range unknown {
		detail := fmt.Sprintf("%q is not a property lodo reads (it reads duration::, time-map::, priority::, plan-start::, plan-end::, deadline::, scheduled::)", key)
		if text, ok := propsText(blockProps(b)[key]); ok && text != "" {
			detail += fmt.Sprintf(" Value %q is left alone.", text)
		}
		r.issue(IssueUnknownProperty, r.blockContext(page, b), detail)
	}

	return t, true, nil
}

// value resolves a property for the block: its own value, else the page's
// inherited default when the key inherits (logseq-integration.md), else the
// fallback "". An empty resolved text is treated as absent everywhere, so
// an explicitly empty value behaves like none.
func (r *resolver) value(page *logseq.Page, b *logseq.Block, key string) string {
	if raw, ok := blockProps(b)[key]; ok {
		text, okT := propsText(raw)
		if !okT {
			r.issue(IssueBadProperty, r.blockContext(page, b),
				fmt.Sprintf("%s:: has a malformed value the app parsed as %v; the property is skipped", key, raw))
			return ""
		}
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			return trimmed
		}
	}
	if !inheritableKeys[key] || page == nil {
		return ""
	}
	return r.pageDefault(page.Name, key, "")
}

// blockValue is the block-level value only — for keys that never inherit.
func (r *resolver) blockValue(page *logseq.Page, b *logseq.Block, key string) string {
	return r.blockOneValue(page, b, key)
}

func (r *resolver) blockOneValue(page *logseq.Page, b *logseq.Block, key string) string {
	if raw, ok := blockProps(b)[key]; ok {
		text, okT := propsText(raw)
		if !okT {
			r.issue(IssueBadProperty, r.blockContext(page, b),
				fmt.Sprintf("%s:: has a malformed value the app parsed as %v; the property is skipped", key, raw))
			return ""
		}
		return strings.TrimSpace(text)
	}
	return ""
}

// pageDefault reads or sets the inheritable page default for key on page.
func (r *resolver) pageDefault(pageName, key, text string) string {
	at := fmt.Sprintf("%s\x00%s", strings.ToLower(strings.TrimSpace(pageName)), key)
	if text == "" {
		return r.pageProps[at]
	}
	r.pageProps[at] = strings.TrimSpace(text)
	return text
}

// splitSlugs parses a time-map:: value "deep-work, weekends" into its
// case-normalised slugs; empty and blank pieces vanish.
func splitSlugs(text string) []string {
	var out []string
	for _, piece := range strings.Split(text, ",") {
		if slug := parse.Slugify(piece); slug != "" {
			out = append(out, slug)
		}
	}
	return out
}

// parseDate accepts the native org stamp and the bare date for deadline::.
func parseDate(s string) (model.CivilDate, error) {
	if stamp, err := parse.ParseOrgStamp(s); err == nil {
		return stamp.Date, nil
	}
	return parse.ParseCivilDate(s)
}

// taskText is the block's display text: its first non-property line with
// the marker prefix removed.
func taskText(b *logseq.Block) string {
	text := stripProps(b.Content)
	if text == "" {
		return ""
	}
	first, _, _ := strings.Cut(text, "\n")
	first = strings.TrimSpace(first)
	if marker := strings.TrimSpace(b.Marker); marker != "" && len(first) > len(marker) &&
		strings.EqualFold(first[:len(marker)], marker) {
		first = strings.TrimSpace(first[len(marker):])
	}
	return first
}

// appendBad adds a bad-property-value issue with the value quoted.
func (r *resolver) appendBad(context, key, raw string, err error) {
	r.issue(IssueBadProperty, context, fmt.Sprintf("%s:: %q: %v", key, raw, err))
}

// containsString reports whether want is in list.
func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
