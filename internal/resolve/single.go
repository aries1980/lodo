package resolve

import (
	"fmt"
	"slices"
	"strings"

	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/norm"
	"github.com/aries1980/lodo/internal/parse"
)

// taskWithAncestors resolves one candidate block with ancestor-aware
// inheritance: time-map, priority, plan-start/end and order cascade
// block > ancestors (nearest first) > page; duration, deadline, scheduled
// and depends never inherit.
func (r *resolver) taskWithAncestors(page *logseq.Page, block *logseq.Block, ancestors []*logseq.Block) (model.Task, bool, *Issue) {
	task := model.Task{
		ID:         strings.TrimSpace(block.UUID),
		Graph:      r.in.Graph,
		GraphOrder: r.in.GraphOrder,
		Text:       taskText(block),
		Marker:     model.Marker(strings.ToUpper(strings.TrimSpace(block.Marker))),
	}
	prov := TaskProvenance{TaskID: task.ID}
	addProv := func(prop, value, source, sourceID string, inherited bool) {
		if value == "" && source == "" {
			return
		}
		prov.Properties = append(prov.Properties, PropertyProvenance{
			Property: prop, Value: value, Source: source, SourceID: sourceID, Inherited: inherited,
		})
	}

	if task.ID == "" {
		return task, false, &Issue{
			Kind:    IssueMissingBlockID,
			Context: r.pageContext(page.Name),
			Detail:  "the task-marked block has no UUID, so it cannot be tracked across runs",
		}
	}

	rawDur, ok := blockProps(block)["duration"]
	if !ok {
		return task, false, &Issue{
			Kind:    IssueMissingDuration,
			Context: r.blockContext(page, block),
			Detail:  fmt.Sprintf("%q has no duration::, so it is not scheduled; a TODO becomes a Task only with an explicit duration (ADR 0004)", task.Text),
		}
	}
	durText, ok := propsText(rawDur)
	if !ok {
		return task, false, &Issue{
			Kind:    IssueBadProperty,
			Context: r.blockContext(page, block),
			Detail:  fmt.Sprintf("duration:: has a malformed value the app parsed as %v; the block is not scheduled", rawDur),
		}
	}
	dur, durErr := parse.ParseDuration(durText)
	if durErr != nil {
		return task, false, &Issue{
			Kind:    IssueBadProperty,
			Context: r.blockContext(page, block),
			Detail:  fmt.Sprintf("duration:: %q is unparseable (%v); the block is not scheduled", durText, durErr),
		}
	}
	task.Duration = dur
	addProv("duration", durText, "direct", "", false)

	if text, source, sourceID := r.inheritedValue(page, block, ancestors, "time-map"); text != "" {
		for _, slug := range splitSlugs(text) {
			if !slices.Contains(task.TimeMaps, slug) {
				task.TimeMaps = append(task.TimeMaps, slug)
			}
		}
		addProv("time-map", text, source, sourceID, source != "direct")
	} else if r.in.DefaultTimeMap != "" {
		// Graph-level fallback: explicit time-map:: on block, parent or
		// page wins; otherwise the graph default applies before the
		// global default:: true map.
		if slug := parse.Slugify(r.in.DefaultTimeMap); slug != "" {
			task.TimeMaps = []string{slug}
			addProv("time-map", r.in.DefaultTimeMap, "graph-default", "", true)
		}
	} else {
		addProv("time-map", "(global default Time Map)", "default-time-map", "", true)
	}

	if text, source, sourceID := r.inheritedValue(page, block, ancestors, "priority"); text != "" {
		p, pErr := parse.ParsePriority(text)
		if pErr != nil {
			r.appendBad(r.blockContext(page, block), "priority", text, pErr)
			addProv("priority", text, source, sourceID, source != "direct")
		} else {
			task.Priority = p
			addProv("priority", string(p), source, sourceID, source != "direct")
		}
	}

	if text, source := r.blockValue(page, block, "deadline"); text != "" {
		d, dErr := parseDate(text)
		if dErr != nil {
			r.appendBad(r.blockContext(page, block), "deadline", text, dErr)
			addProv("deadline", text, source, "", false)
		} else {
			task.Deadline = &d
			addProv("deadline", d.String(), source, "", false)
		}
	}

	if text, source, sourceID := r.inheritedValue(page, block, ancestors, "plan-start"); text != "" {
		d, pErr := parse.ParseCivilDate(text)
		if pErr != nil {
			r.appendBad(r.blockContext(page, block), "plan-start", text, pErr)
			addProv("plan-start", text, source, sourceID, source != "direct")
		} else {
			task.PlanStart = &d
			addProv("plan-start", d.String(), source, sourceID, source != "direct")
		}
	}
	if text, source, sourceID := r.inheritedValue(page, block, ancestors, "plan-end"); text != "" {
		d, pErr := parse.ParseCivilDate(text)
		if pErr != nil {
			r.appendBad(r.blockContext(page, block), "plan-end", text, pErr)
			addProv("plan-end", text, source, sourceID, source != "direct")
		} else {
			task.PlanEnd = &d
			addProv("plan-end", d.String(), source, sourceID, source != "direct")
		}
	}

	if text, source, sourceID := r.inheritedValue(page, block, ancestors, "order"); text != "" {
		m, oErr := parse.ParseOrder(text)
		if oErr != nil {
			r.appendBad(r.blockContext(page, block), "order", text, oErr)
			addProv("order", text, source, sourceID, source != "direct")
		} else if m == model.OrderSequential {
			task.Order = m
			addProv("order", string(m), source, sourceID, source != "direct")
		} else {
			addProv("order", string(m), source, sourceID, source != "direct")
		}
	} else {
		addProv("order", string(model.OrderParallel), "implicit", "", true)
	}

	if text, source := r.blockValue(page, block, "scheduled"); text != "" {
		stamp, sErr := parse.ParseOrgStamp(text)
		switch {
		case sErr != nil:
			r.appendBad(r.blockContext(page, block), "scheduled", text, sErr)
			addProv("scheduled", text, source, "", false)
		case !stamp.HasTime:
			r.appendBad(r.blockContext(page, block), "scheduled", text,
				fmt.Errorf("no time: a Pin is an exact date-time and the day-granular equivalent is plan-start or plan-end"))
			addProv("scheduled", text, source, "", false)
		default:
			pin, pinErr := stamp.Instant(r.in.Location)
			if pinErr != nil {
				r.appendBad(r.blockContext(page, block), "scheduled", text, pinErr)
				addProv("scheduled", text, source, "", false)
				break
			}
			task.Pin = &pin
			addProv("scheduled", pin.Format("2006-01-02 15:04 MST"), source, "", false)
		}
	}

	// depends:: — block-level only, strict comma ((uuid)) list.
	if text, source := r.blockValue(page, block, "depends"); text != "" {
		deps, dErr := parse.ParseDepends(text)
		if dErr != nil {
			r.appendBad(r.blockContext(page, block), "depends", text, dErr)
			task.Depends = []string{fmt.Sprintf("malformed-depends:%s", norm.ID(task.ID))}
			addProv("depends", text, source, "", false)
		} else {
			task.Depends = deps
			addProv("depends", strings.Join(deps, ", "), source, "", false)
		}
	}

	var unknown []string
	for key, v := range blockProps(block) {
		if !knownKeys[key] {
			unknown = append(unknown, key)
			continue
		}
		_ = v
	}
	slices.Sort(unknown)
	for _, key := range unknown {
		detail := fmt.Sprintf("%q is not a property lodo reads (it reads duration::, time-map::, priority::, plan-start::, plan-end::, deadline::, scheduled::, depends::, order::)", key)
		if text, ok := propsText(blockProps(block)[key]); ok && text != "" {
			detail += fmt.Sprintf(" Value %q is left alone.", text)
		}
		r.issue(IssueUnknownProperty, r.blockContext(page, block), detail)
	}

	r.provenance[task.ID] = prov
	return task, true, nil
}

// inheritedValue resolves an inheritable property: block explicit, then
// ancestors nearest-first, then the page default. A blank explicit value
// falls through; a malformed block value issues and stops (ancestors are
// skipped, matching historic behavior).
func (r *resolver) inheritedValue(page *logseq.Page, block *logseq.Block, ancestors []*logseq.Block, key string) (text, source, sourceID string) {
	if raw, ok := blockProps(block)[key]; ok {
		t, okT := propsText(raw)
		if !okT {
			r.issue(IssueBadProperty, r.blockContext(page, block),
				fmt.Sprintf("%s:: has a malformed value the app parsed as %v; the property is skipped", key, raw))
			return "", "direct", ""
		}
		if trimmed := strings.TrimSpace(t); trimmed != "" {
			return trimmed, "direct", ""
		}
	}
	for k := len(ancestors) - 1; k >= 0; k-- {
		if v := blockTextOnly(ancestors[k], key); v != "" {
			return v, "ancestor", ancestors[k].UUID
		}
	}
	if !inheritableKeys[key] || page == nil {
		return "", "", ""
	}
	if v := r.pageDefault(page.Name, key, ""); v != "" {
		return v, "page", page.Name
	}
	return "", "", ""
}

// blockValue is the block-level value only — for keys that never inherit.
func (r *resolver) blockValue(page *logseq.Page, block *logseq.Block, key string) (text, source string) {
	t := r.blockText(page, block, key, true)
	if t != "" {
		return t, "direct"
	}
	return "", ""
}

// blockText reads one block's property text, reporting malformed values.
func (r *resolver) blockText(page *logseq.Page, block *logseq.Block, key string, report bool) string {
	raw, ok := blockProps(block)[key]
	if !ok {
		return ""
	}
	text, okT := propsText(raw)
	if !okT {
		if report {
			r.issue(IssueBadProperty, r.blockContext(page, block),
				fmt.Sprintf("%s:: has a malformed value the app parsed as %v; the property is skipped", key, raw))
		}
		return ""
	}
	return strings.TrimSpace(text)
}

func blockTextOnly(block *logseq.Block, key string) string {
	raw, ok := blockProps(block)[key]
	if !ok {
		return ""
	}
	text, okT := propsText(raw)
	if !okT {
		return ""
	}
	return strings.TrimSpace(text)
}

// pageDefault reads or sets the inheritable page default for key on page.
func (r *resolver) pageDefault(pageName, key, text string) string {
	at := fmt.Sprintf("%s\x00%s", norm.ID(pageName), key)
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
func taskText(block *logseq.Block) string {
	text := stripProps(block.Content)
	if text == "" {
		return ""
	}
	first, _, _ := strings.Cut(text, "\n")
	first = strings.TrimSpace(first)
	if marker := strings.TrimSpace(block.Marker); marker != "" && len(first) > len(marker) &&
		strings.EqualFold(first[:len(marker)], marker) {
		first = strings.TrimSpace(first[len(marker):])
	}
	return first
}

// appendBad adds a bad-property-value issue with the value quoted.
func (r *resolver) appendBad(context, key, raw string, err error) {
	r.issue(IssueBadProperty, context, fmt.Sprintf("%s:: %q: %v", key, raw, err))
}
