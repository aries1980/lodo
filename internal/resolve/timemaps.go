package resolve

import (
	"fmt"
	"strings"

	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/parse"
)

// timeMaps resolves the Time Maps page (ADR 0003): one top-level block per
// map (its text slugifies case-insensitively into the key), one child per
// window line, default:: true on exactly one map.
func (r *resolver) timeMaps() {
	idx := r.pageIndex(r.in.TimeMapsPage)
	if idx < 0 {
		r.issue(IssueMissingTimeMapsPage,
			fmt.Sprintf("page %s", r.in.TimeMapsPage),
			fmt.Sprintf("no page named %q: the Time Maps are authored in the graph on it (ADR 0003)", r.in.TimeMapsPage))
		return
	}

	tree := r.treeFor(r.in.Pages[idx].Name)
	// An empty Time Maps page reads as zero maps; the no-default rule below
	// then reports, so the absence is visible exactly once.
	defaults := 0
	for _, mb := range tree {
		name := strings.TrimSpace(stripProps(mb.Content))
		slug := parse.Slugify(name)
		if slug == "" {
			r.issue(IssueBadTimeMap, r.tmBlockContext(mb),
				fmt.Sprintf("map text %q slugs to an empty key; give the map a name", name))
			continue
		}
		if r.mapSlugs[slug] {
			r.issue(IssueDuplicateMap, r.tmBlockContext(mb),
				fmt.Sprintf("%q slugs to %s, already defined above on the page; the later block is skipped", name, slug))
			continue
		}

		m := model.TimeMap{Slug: slug}
		defined, err := isTrue(mb, "default")
		switch {
		case err != nil:
			r.issue(IssueBadDefault, r.tmBlockContext(mb), err.Error())
		case defined:
			m.Default = true
			defaults++
		}

		// Each child block = one window line. Grandchildren are not part of
		// a map in v0.1.
		for _, wb := range mb.Children {
			line := strings.TrimSpace(stripProps(wb.Content))
			w, err := parse.ParseWindow(line)
			if err != nil {
				r.issue(IssueBadWindow, r.tmBlockContext(wb), fmt.Sprintf("window %q in map %s: %v", line, slug, err))
				continue
			}
			m.Windows = append(m.Windows, w)
		}

		r.maps = append(r.maps, m)
		r.mapSlugs[slug] = true
	}

	switch {
	case defaults == 0:
		r.issue(IssueDefaultMapCount, fmt.Sprintf("page %s", r.in.TimeMapsPage),
			"no Time Map carries default:: true, so Tasks with no time-map:: have no fallback hours")
	case defaults > 1:
		var slugs []string
		for _, m := range r.maps {
			if m.Default {
				slugs = append(slugs, m.Slug)
			}
		}
		r.issue(IssueDefaultMapCount, fmt.Sprintf("page %s", r.in.TimeMapsPage),
			fmt.Sprintf("%d Time Maps have default:: true (%s); exactly one must — none applies", defaults, strings.Join(slugs, ", ")))
		// More than one default is a guess if any is used: strip them all.
		for i := range r.maps {
			r.maps[i].Default = false
		}
	}
}

// isTrue resolves a boolean-valued property on a block: any of true/false,
// or empty (absent). Other values are a malformed flag.
func isTrue(b *logseq.Block, key string) (bool, error) {
	props := blockProps(b)
	raw, ok := props[key]
	if !ok {
		return false, nil
	}
	v, ok := propsText(raw)
	if !ok {
		return false, fmt.Errorf("a malformed %s:: value", key)
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return false, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("default:: %q: want true or false", v)
	}
}
