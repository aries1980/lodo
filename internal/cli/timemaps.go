// The prompted bootstrap of a missing Time Maps page: when the configured
// page is absent, check/forecast/schedule offer to create it with the
// recommended defaults below, which the user then edits in Logseq. This is
// Lodo's one graph write, and it only ever happens on an explicit yes —
// interactively, or up front with -y/--yes. See ADR 0015.
package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aries1980/lodo/internal/logseq"
)

// timeMapSeed is one recommended Time Map: a name, its window lines, and
// whether it is the default that Tasks without a time-map:: fall back to.
type timeMapSeed struct {
	Name    string
	Default bool
	Windows []string
}

// defaultTimeMaps are the recommended Time Maps offered for a missing page
// (time-maps.md): weekday mornings with a yellow Saturday spill for Deep
// Work, a Saturday errand run, and yellow weekends.
func defaultTimeMaps() []timeMapSeed {
	return []timeMapSeed{
		{Name: "Deep Work", Default: true, Windows: []string{
			"Mon-Fri 09:00-11:00",
			"Sat 10:00-12:00 yellow",
		}},
		{Name: "Errands", Windows: []string{
			"Sat 10:00-13:00",
		}},
		{Name: "Weekends", Windows: []string{
			"Sat-Sun 08:00-20:00 yellow",
		}},
	}
}

// ensureTimeMapsPage creates the configured Time Maps page with the
// recommended defaults when it is missing, and reports whether it did. An
// existing page is left alone. Without yes it asks first; anything but an
// explicit y/yes — including EOF — declines, and the run continues without
// the page. Prompts and notes go to errW, so --json stdout stays
// machine-readable.
func ensureTimeMapsPage(ctx context.Context, client logseq.Client, page string, yes bool, errW io.Writer) (bool, error) {
	found, err := client.Page(ctx, page)
	if err != nil {
		return false, err
	}
	if found != nil {
		return false, nil
	}
	if !yes && !confirmCreateTimeMaps(page, errW) {
		return false, nil
	}
	if err := createTimeMapsPage(ctx, client, page); err != nil {
		return false, err
	}
	_, _ = fmt.Fprintf(errW,
		"Created Time Maps page %q with the recommended defaults; edit it in Logseq to suit your week.\n",
		page)
	return true, nil
}

// confirmCreateTimeMaps asks whether the missing page may be created. Only
// an explicit y/yes counts; the empty default, anything else, and EOF all
// decline.
func confirmCreateTimeMaps(page string, errW io.Writer) bool {
	_, _ = fmt.Fprintf(errW,
		"Time Maps page %q is missing: Tasks with no time-map:: have no fallback hours.\n"+
			"Create it with the recommended defaults (Deep Work as the default map, plus Errands and Weekends)? You can edit it afterwards in Logseq. [y/N] ",
		page)
	line, err := bufio.NewReader(stdin()).ReadString('\n')
	if err != nil && len(line) == 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

// createTimeMapsPage writes the recommended defaults onto a fresh page: one
// top-level block per map (ADR 0003), one child block per window line, and
// default:: true on exactly one map.
func createTimeMapsPage(ctx context.Context, client logseq.Client, page string) error {
	if _, err := client.CreatePage(ctx, page); err != nil {
		return fmt.Errorf("create Time Maps page %q: %w", page, err)
	}
	for i, seed := range defaultTimeMaps() {
		uuid, err := placeMapBlock(ctx, client, page, seed.Name, i == 0)
		if err != nil {
			return err
		}
		if seed.Default {
			if err := client.SetBlockProperty(ctx, uuid, "default", true); err != nil {
				return fmt.Errorf("mark %q as the default map: %w", seed.Name, err)
			}
		}
		for _, window := range seed.Windows {
			if _, err := client.InsertBlock(ctx, uuid, window); err != nil {
				return fmt.Errorf("add window %q to %q: %w", window, seed.Name, err)
			}
		}
	}
	return nil
}

// placeMapBlock writes one top-level map block. A freshly created page may
// already hold one empty block (the file graph does); the first map reuses
// it in place, since an extra blank block would read as a map with no name.
func placeMapBlock(ctx context.Context, client logseq.Client, page, name string, first bool) (string, error) {
	if first {
		tree, err := client.Tree(ctx, page)
		if err != nil {
			return "", fmt.Errorf("read fresh Time Maps page %q: %w", page, err)
		}
		if len(tree) == 1 && strings.TrimSpace(tree[0].Content) == "" && len(tree[0].Children) == 0 {
			if err := client.UpdateBlock(ctx, tree[0].UUID, name); err != nil {
				return "", fmt.Errorf("write map %q: %w", name, err)
			}
			return tree[0].UUID, nil
		}
	}
	block, err := client.AppendBlock(ctx, page, name)
	if err != nil {
		return "", fmt.Errorf("write map %q: %w", name, err)
	}
	if block == nil {
		return "", fmt.Errorf("write map %q: the app returned no block", name)
	}
	return block.UUID, nil
}
