package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/parse"
)

// memClient is an in-memory Logseq graph for the bootstrap tests: pages and
// trees the test seeds by hand, writes recorded in call order.
type memClient struct {
	pages  map[string]*logseq.Page
	trees  map[string][]*logseq.Block
	writes []string
	seq    atomic.Int64
}

func newMemClient() *memClient {
	return &memClient{pages: map[string]*logseq.Page{}, trees: map[string][]*logseq.Block{}}
}

func (c *memClient) nextUUID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, c.seq.Add(1))
}

func (c *memClient) Pages(context.Context) ([]*logseq.Page, error) {
	var out []*logseq.Page
	for _, p := range c.pages {
		out = append(out, &logseq.Page{Name: p.Name})
	}
	return out, nil
}

func (c *memClient) Page(_ context.Context, name string) (*logseq.Page, error) {
	if p, ok := c.pages[name]; ok {
		return &logseq.Page{Name: p.Name}, nil
	}
	return nil, nil
}

func (c *memClient) Tree(_ context.Context, name string) ([]*logseq.Block, error) {
	return c.trees[name], nil
}

func (c *memClient) CreatePage(_ context.Context, name string) (*logseq.Page, error) {
	c.writes = append(c.writes, "create "+name)
	c.pages[name] = &logseq.Page{Name: name}
	// The file graph answers a fresh page with one empty block.
	empty := &logseq.Block{UUID: c.nextUUID("b"), Content: ""}
	c.trees[name] = []*logseq.Block{empty}
	return &logseq.Page{Name: name}, nil
}

func (c *memClient) AppendBlock(_ context.Context, page, content string) (*logseq.Block, error) {
	c.writes = append(c.writes, "append "+page+" "+content)
	b := &logseq.Block{UUID: c.nextUUID("b"), Content: content}
	c.trees[page] = append(c.trees[page], b)
	return b, nil
}

func (c *memClient) UpdateBlock(_ context.Context, uuid, content string) error {
	c.writes = append(c.writes, "update "+uuid+" "+content)
	for _, blocks := range c.trees {
		for _, b := range blocks {
			if b.UUID == uuid {
				b.Content = content
				return nil
			}
		}
	}
	return errors.New("memClient: no such block " + uuid)
}

func (c *memClient) InsertBlock(_ context.Context, parentUUID, content string) (*logseq.Block, error) {
	c.writes = append(c.writes, "insert "+parentUUID+" "+content)
	child := &logseq.Block{UUID: c.nextUUID("w"), Content: content}
	for _, blocks := range c.trees {
		for _, b := range blocks {
			if b.UUID == parentUUID {
				b.Children = append(b.Children, child)
				return child, nil
			}
		}
	}
	return nil, errors.New("memClient: no such parent " + parentUUID)
}

func (c *memClient) SetBlockProperty(_ context.Context, uuid, key string, value any) error {
	c.writes = append(c.writes, fmt.Sprintf("prop %s %s=%v", uuid, key, value))
	for _, blocks := range c.trees {
		for _, b := range blocks {
			if b.UUID == uuid {
				if b.Properties == nil {
					b.Properties = map[string]any{}
				}
				b.Properties[key] = value
				return nil
			}
		}
	}
	return errors.New("memClient: no such block " + uuid)
}

// withStdin swaps the prompt input for one test.
func withStdin(t *testing.T, input string) {
	t.Helper()
	old := stdin
	stdin = func() io.Reader { return strings.NewReader(input) }
	t.Cleanup(func() { stdin = old })
}

// failingStdin explodes if anything reads: the helper must not prompt when
// the page exists or -y is given.
func failingStdin(t *testing.T) {
	t.Helper()
	old := stdin
	stdin = func() io.Reader {
		return readerFunc(func([]byte) (int, error) { panic("unexpected prompt read") })
	}
	t.Cleanup(func() { stdin = old })
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func TestEnsureExistingPageUntouched(t *testing.T) {
	c := newMemClient()
	c.pages["Time Maps"] = &logseq.Page{Name: "Time Maps"}
	c.trees["Time Maps"] = []*logseq.Block{{UUID: "m-1", Content: "Deep Work"}}
	failingStdin(t)

	errW := &strings.Builder{}
	created, err := ensureTimeMapsPage(context.Background(), c, "Time Maps", false, errW, styler{})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if created {
		t.Fatal("an existing page must not be recreated")
	}
	if len(c.writes) != 0 {
		t.Fatalf("writes = %v, want none", c.writes)
	}
	if errW.Len() != 0 {
		t.Fatalf("prompted on an existing page: %q", errW.String())
	}
}

func TestEnsurePromptYesCreatesDefaults(t *testing.T) {
	c := newMemClient()
	withStdin(t, "y\n")

	errW := &strings.Builder{}
	created, err := ensureTimeMapsPage(context.Background(), c, "Time Maps", false, errW, styler{})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !created {
		t.Fatal("an explicit yes must create the page")
	}
	if !strings.Contains(errW.String(), "[y/N]") {
		t.Fatalf("no prompt was shown: %q", errW.String())
	}
	// The file graph's empty first block is reused in place: update, not
	// append, carries the first map.
	if len(c.writes) == 0 || c.writes[0] != "create Time Maps" {
		t.Fatalf("writes = %v, want create first", c.writes)
	}
	if len(c.writes) < 2 || !strings.HasPrefix(c.writes[1], "update ") {
		t.Fatalf("writes = %v, want the empty first block reused", c.writes)
	}
	assertDefaultTimeMaps(t, c)
}

func TestEnsureYesFlagSkipsPrompt(t *testing.T) {
	c := newMemClient()
	failingStdin(t)

	errW := &strings.Builder{}
	created, err := ensureTimeMapsPage(context.Background(), c, "Time Maps", true, errW, styler{})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !created {
		t.Fatal("-y must create the page without asking")
	}
	if strings.Contains(errW.String(), "[y/N]") {
		t.Fatalf("prompt shown despite -y: %q", errW.String())
	}
	assertDefaultTimeMaps(t, c)
}

func TestEnsurePromptNoLeavesGraphAlone(t *testing.T) {
	for _, input := range []string{"n\n", "\n", "", "maybe\n"} {
		c := newMemClient()
		withStdin(t, input)

		errW := &strings.Builder{}
		created, err := ensureTimeMapsPage(context.Background(), c, "Time Maps", false, errW, styler{})
		if err != nil {
			t.Fatalf("ensure(%q): %v", input, err)
		}
		if created {
			t.Fatalf("input %q must decline", input)
		}
		if len(c.writes) != 0 {
			t.Fatalf("input %q wrote %v", input, c.writes)
		}
		if _, ok := c.pages["Time Maps"]; ok {
			t.Fatalf("input %q created the page", input)
		}
	}
}

// assertDefaultTimeMaps checks the created page against the contract the
// resolver enforces: three maps, exactly one default, every window parses.
func assertDefaultTimeMaps(t *testing.T, c *memClient) {
	t.Helper()
	tree := c.trees["Time Maps"]
	if len(tree) != 3 {
		t.Fatalf("top-level blocks = %d, want 3 maps (writes %v)", len(tree), c.writes)
	}
	defaults := 0
	seen := map[string]bool{}
	for _, m := range tree {
		name := m.Content
		if i := strings.Index(name, "\n"); i >= 0 {
			name = name[:i]
		}
		if name == "" || seen[name] {
			t.Fatalf("map name %q is empty or duplicated", m.Content)
		}
		seen[name] = true
		if len(m.Children) == 0 {
			t.Fatalf("map %q has no windows", name)
		}
		for _, w := range m.Children {
			if _, err := parse.ParseWindow(w.Content); err != nil {
				t.Fatalf("window %q of %q does not parse: %v", w.Content, name, err)
			}
		}
		if d, ok := m.Properties["default"]; ok && d == true {
			defaults++
		}
	}
	if defaults != 1 {
		t.Fatalf("defaults = %d, want exactly 1 (writes %v)", defaults, c.writes)
	}
	if c.pages["Time Maps"] == nil {
		t.Fatal("the page itself was never created")
	}
}

func TestDefaultTimeMapsContract(t *testing.T) {
	seeds := defaultTimeMaps()
	if len(seeds) != 3 {
		t.Fatalf("seeds = %d, want 3", len(seeds))
	}
	defaults := 0
	for _, s := range seeds {
		if s.Default {
			defaults++
		}
		if len(s.Windows) == 0 {
			t.Fatalf("seed %q has no windows", s.Name)
		}
		for _, w := range s.Windows {
			if _, err := parse.ParseWindow(w); err != nil {
				t.Fatalf("seed window %q does not parse: %v", w, err)
			}
		}
	}
	if defaults != 1 {
		t.Fatalf("defaults = %d, want exactly 1", defaults)
	}
}
