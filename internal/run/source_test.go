package run_test

import (
	stdcmp "cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/norm"
	"github.com/aries1980/lodo/internal/resolve"
	"github.com/aries1980/lodo/internal/run"
)

// loadGraphSequential is the pre-fan-out LoadGraph, kept as the oracle for
// TestLoadGraphMatchesSequentialRead. Do not simplify it to call run.LoadGraph:
// the duplicate is what pins the output.
func loadGraphSequential(ctx context.Context, source run.Source, timeMapsPage string, loc *time.Location) (resolve.Result, resolve.Input, map[string]*logseq.Block, map[string]string, error) {
	pages, err := source.Client.Pages(ctx)
	if err != nil {
		return resolve.Result{}, resolve.Input{}, nil, nil, err
	}
	sortPagesForTest(pages)

	tmName := norm.ID(timeMapsPage)
	input := resolve.Input{
		Graph:          source.Name,
		GraphOrder:     source.Order,
		DefaultTimeMap: source.DefaultTimeMap,
		Pages:          pages,
		Trees:          make(map[string][]*logseq.Block, len(pages)),
		TimeMapsPage:   timeMapsPage,
		Location:       loc,
	}
	for _, p := range pages {
		tree, err := source.Client.Tree(ctx, p.Name)
		if err != nil {
			return resolve.Result{}, resolve.Input{}, nil, nil, err
		}
		input.Trees[p.Name] = tree
		if norm.ID(p.Name) != tmName && !hasCandidateForTest(tree) {
			continue
		}
		info, err := source.Client.Page(ctx, p.Name)
		if err != nil {
			return resolve.Result{}, resolve.Input{}, nil, nil, err
		}
		if info == nil {
			continue
		}
		p.Properties = info.Properties
	}
	res, err := resolve.Resolve(input)
	if err != nil {
		return resolve.Result{}, resolve.Input{}, nil, nil, err
	}
	blocks := make(map[string]*logseq.Block, len(res.Tasks))
	pageOf := make(map[string]string, len(res.Tasks))
	for _, p := range pages {
		tree := input.Trees[p.Name]
		_ = logseq.Walk(tree, func(b *logseq.Block) error {
			if b.UUID == "" {
				return nil
			}
			if _, ok := blocks[b.UUID]; !ok {
				blocks[b.UUID] = b
				pageOf[b.UUID] = p.Name
			}
			return nil
		})
	}
	return res, input, blocks, pageOf, nil
}

func sortPagesForTest(pages []*logseq.Page) {
	slices.SortFunc(pages, func(a, b *logseq.Page) int {
		return stdcmp.Or(stdcmp.Compare(norm.ID(a.Name), norm.ID(b.Name)), stdcmp.Compare(a.Name, b.Name))
	})
}

func hasCandidateForTest(tree []*logseq.Block) bool {
	found := false
	_ = logseq.Walk(tree, func(b *logseq.Block) error {
		switch strings.ToUpper(strings.TrimSpace(b.Marker)) {
		case "TODO", "DOING":
			found = true
		}
		return nil
	})
	return found
}

func TestLoadGraphMatchesSequentialRead(t *testing.T) {
	tests := []struct {
		name         string
		fixture      graphFixture
		timeMapsPage string
	}{
		{name: "fixture week", fixture: fixtureWeek(), timeMapsPage: "Time Maps"},
		{name: "empty graph", fixture: graphFixture{}, timeMapsPage: "Time Maps"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantRes, wantInput, wantBlocks, wantPageOf, wantErr := loadGraphSequential(
				t.Context(), graphSource("work", 0, tt.fixture, "deep-work"), tt.timeMapsPage, london)
			gotRes, gotInput, gotBlocks, gotPageOf, gotErr := run.LoadGraph(
				t.Context(), graphSource("work", 0, tt.fixture, "deep-work"), tt.timeMapsPage, london)

			if (wantErr != nil) != (gotErr != nil) {
				t.Fatalf("errors = %v, want %v", gotErr, wantErr)
			}
			if wantErr != nil && wantErr.Error() != gotErr.Error() {
				t.Fatalf("error = %q, want %q", gotErr, wantErr)
			}
			if diff := cmp.Diff(wantRes, gotRes); diff != "" {
				t.Fatalf("result mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(wantInput, gotInput, cmpopts.IgnoreFields(resolve.Input{}, "Location")); diff != "" {
				t.Fatalf("input mismatch (-want +got):\n%s", diff)
			}
			if gotInput.Location != london || wantInput.Location != london {
				t.Fatalf("locations = %v/%v, want London", wantInput.Location, gotInput.Location)
			}
			if diff := cmp.Diff(wantBlocks, gotBlocks); diff != "" {
				t.Fatalf("blocks mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(wantPageOf, gotPageOf); diff != "" {
				t.Fatalf("pageOf mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// barrierClient blocks every Tree call until the test releases it, so a
// serial implementation can only ever have one read in flight.
type barrierClient struct {
	*stubClient
	started chan<- string
	release <-chan struct{}
}

func (c *barrierClient) Tree(ctx context.Context, name string) ([]*logseq.Block, error) {
	c.started <- name
	<-c.release
	return c.stubClient.Tree(ctx, name)
}

func TestLoadGraphReadsPagesConcurrently(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fixture := graphFixture{
			pages: []*logseq.Page{memPage("a"), memPage("b"), memPage("c"), memPage("d")},
			trees: map[string][]*logseq.Block{
				"a": {block("a-1", "plain a")},
				"b": {block("b-1", "plain b")},
				"c": {block("c-1", "plain c")},
				"d": {block("d-1", "plain d")},
			},
		}
		started := make(chan string, len(fixture.pages))
		release := make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		defer unblock()

		client := &barrierClient{stubClient: newStubClient(fixture), started: started, release: release}
		done := make(chan error, 1)
		go func() {
			_, _, _, _, err := run.LoadGraph(t.Context(), run.Source{Client: client}, "Time Maps", london)
			done <- err
		}()

		synctest.Wait()
		if got := len(started); got != len(fixture.pages) {
			t.Fatalf("tree reads in flight = %d, want %d", got, len(fixture.pages))
		}
		unblock()
		synctest.Wait()
		if err := <-done; err != nil {
			t.Fatalf("LoadGraph: %v", err)
		}
	})
}

// failingClient fails the first sorted page and lets every other tree read
// block until the fan-out cancels it.
type failingClient struct {
	pages   []*logseq.Page
	started chan string
	err     error
}

func (c *failingClient) Pages(context.Context) ([]*logseq.Page, error) { return c.pages, nil }

func (c *failingClient) Ping(context.Context) error { return nil }

func (c *failingClient) Page(context.Context, string) (*logseq.Page, error) {
	return &logseq.Page{}, nil
}

func (c *failingClient) Tree(ctx context.Context, name string) ([]*logseq.Block, error) {
	if name == "a" {
		return nil, c.err
	}
	c.started <- name
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *failingClient) CreatePage(context.Context, string) (*logseq.Page, error) {
	return nil, errors.New("failingClient: writes unsupported")
}

func (c *failingClient) AppendBlock(context.Context, string, string) (*logseq.Block, error) {
	return nil, errors.New("failingClient: writes unsupported")
}

func (c *failingClient) UpdateBlock(context.Context, string, string) error {
	return errors.New("failingClient: writes unsupported")
}

func (c *failingClient) InsertBlock(context.Context, string, string) (*logseq.Block, error) {
	return nil, errors.New("failingClient: writes unsupported")
}

func (c *failingClient) SetBlockProperty(context.Context, string, string, any) error {
	return errors.New("failingClient: writes unsupported")
}

func TestLoadGraphStopsAfterTheFirstFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		boom := errors.New("boom")
		client := &failingClient{
			pages: []*logseq.Page{
				memPage("a"), memPage("b"), memPage("c"),
				memPage("d"), memPage("e"), memPage("f"),
			},
			started: make(chan string, 6),
			err:     boom,
		}
		_, _, _, _, err := run.LoadGraph(t.Context(), run.Source{Client: client}, "Time Maps", london)
		if !errors.Is(err, boom) {
			t.Fatalf("LoadGraph err = %v, want the first page's failure", err)
		}
		if got, want := len(client.started), len(client.pages)-1; got >= want {
			t.Fatalf("tree reads started = %d, want fewer than %d", got, want)
		}
	})
}

// blockingClient keeps every tree read open until its context is cancelled.
type blockingClient struct {
	pages   []*logseq.Page
	started chan struct{}
}

func (c *blockingClient) Pages(context.Context) ([]*logseq.Page, error) { return c.pages, nil }

func (c *blockingClient) Ping(context.Context) error { return nil }

func (c *blockingClient) Page(context.Context, string) (*logseq.Page, error) {
	return &logseq.Page{}, nil
}

func (c *blockingClient) Tree(ctx context.Context, _ string) ([]*logseq.Block, error) {
	c.started <- struct{}{}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *blockingClient) CreatePage(context.Context, string) (*logseq.Page, error) {
	return nil, errors.New("blockingClient: writes unsupported")
}

func (c *blockingClient) AppendBlock(context.Context, string, string) (*logseq.Block, error) {
	return nil, errors.New("blockingClient: writes unsupported")
}

func (c *blockingClient) UpdateBlock(context.Context, string, string) error {
	return errors.New("blockingClient: writes unsupported")
}

func (c *blockingClient) InsertBlock(context.Context, string, string) (*logseq.Block, error) {
	return nil, errors.New("blockingClient: writes unsupported")
}

func (c *blockingClient) SetBlockProperty(context.Context, string, string, any) error {
	return errors.New("blockingClient: writes unsupported")
}

func TestLoadGraphHonorsCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		client := &blockingClient{
			pages: []*logseq.Page{
				memPage("a"), memPage("b"), memPage("c"), memPage("d"),
				memPage("e"), memPage("f"), memPage("g"), memPage("h"),
			},
			started: make(chan struct{}, 8),
		}
		done := make(chan error, 1)
		go func() {
			_, _, _, _, err := run.LoadGraph(ctx, run.Source{Client: client}, "Time Maps", london)
			done <- err
		}()

		synctest.Wait()
		if got := len(client.started); got == 0 || got >= len(client.pages) {
			t.Fatalf("tree reads started = %d, want some but not all", got)
		}
		cancel()
		synctest.Wait()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("LoadGraph err = %v, want context.Canceled", err)
		}
	})
}

func TestLoadGraphFetchesPagePropertiesOnlyForCandidates(t *testing.T) {
	fixture := graphFixture{
		pages: []*logseq.Page{memPage("a"), memPage("b"), memPage("Time Maps")},
		trees: map[string][]*logseq.Block{
			"a":         {marked("TODO", "t-a", "TODO A")},
			"b":         {block("b-1", "plain b")},
			"Time Maps": {mapDeepWork},
		},
	}
	client := newStubClient(fixture)
	client.props["a"] = &logseq.Page{Name: "a", Properties: map[string]any{"priority": "A"}}

	_, input, _, _, err := run.LoadGraph(t.Context(), run.Source{Client: client}, "Time Maps", london)
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}
	calls := client.PageCalls()
	slices.Sort(calls)
	if want := []string{"Time Maps", "a"}; !slices.Equal(calls, want) {
		t.Fatalf("page calls = %v, want %v", calls, want)
	}
	var pageA *logseq.Page
	for _, p := range input.Pages {
		if p.Name == "a" {
			pageA = p
		}
	}
	if pageA == nil || pageA.Properties["priority"] != "A" {
		t.Fatalf("page a = %+v, want the fetched priority", pageA)
	}
}

func TestLoadGraphEmptyGraph(t *testing.T) {
	res, input, blocks, pageOf, err := run.LoadGraph(
		t.Context(), run.Source{Client: newStubClient(graphFixture{})}, "", london)
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}
	if len(res.Tasks) != 0 || len(input.Trees) != 0 || len(blocks) != 0 || len(pageOf) != 0 {
		t.Fatalf("empty graph produced tasks %d, trees %d, blocks %d, pageOf %d",
			len(res.Tasks), len(input.Trees), len(blocks), len(pageOf))
	}
}
