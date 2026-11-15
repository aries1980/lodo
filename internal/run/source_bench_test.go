package run_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/run"
)

// latencyClient is a graph fixture with a synthetic round-trip time, so the
// benchmark measures the read schedule rather than the resolver.
type latencyClient struct {
	pages     []*logseq.Page
	trees     map[string][]*logseq.Block
	treeDelay time.Duration
	pageDelay time.Duration
}

func (c *latencyClient) Pages(context.Context) ([]*logseq.Page, error) { return c.pages, nil }

func (c *latencyClient) Ping(context.Context) error { return nil }

func (c *latencyClient) Page(ctx context.Context, name string) (*logseq.Page, error) {
	if err := sleepCtx(ctx, c.pageDelay); err != nil {
		return nil, err
	}
	return &logseq.Page{Name: name}, nil
}

func (c *latencyClient) Tree(ctx context.Context, name string) ([]*logseq.Block, error) {
	if err := sleepCtx(ctx, c.treeDelay); err != nil {
		return nil, err
	}
	return c.trees[name], nil
}

func (c *latencyClient) CreatePage(context.Context, string) (*logseq.Page, error) {
	return nil, errors.New("latencyClient: writes unsupported")
}

func (c *latencyClient) AppendBlock(context.Context, string, string) (*logseq.Block, error) {
	return nil, errors.New("latencyClient: writes unsupported")
}

func (c *latencyClient) UpdateBlock(context.Context, string, string) error {
	return errors.New("latencyClient: writes unsupported")
}

func (c *latencyClient) InsertBlock(context.Context, string, string) (*logseq.Block, error) {
	return nil, errors.New("latencyClient: writes unsupported")
}

func (c *latencyClient) SetBlockProperty(context.Context, string, string, any) error {
	return errors.New("latencyClient: writes unsupported")
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func BenchmarkLoadGraph(b *testing.B) {
	const pages = 100
	client := &latencyClient{
		pages:     make([]*logseq.Page, 0, pages+1),
		trees:     make(map[string][]*logseq.Block, pages+1),
		treeDelay: 2 * time.Millisecond,
		pageDelay: time.Millisecond,
	}
	for i := range pages {
		name := fmt.Sprintf("page-%03d", i)
		client.pages = append(client.pages, &logseq.Page{Name: name})
		client.trees[name] = []*logseq.Block{{
			UUID:       fmt.Sprintf("task-%03d", i),
			Content:    "TODO benchmark task",
			Marker:     "TODO",
			Properties: map[string]any{"duration": "30m"},
		}}
	}
	client.pages = append(client.pages, &logseq.Page{Name: "Time Maps"})
	client.trees["Time Maps"] = []*logseq.Block{mapDeepWork}
	source := run.Source{Name: "bench", Client: client, DefaultTimeMap: "deep-work"}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, _, _, _, err := run.LoadGraph(b.Context(), source, "Time Maps", time.UTC); err != nil {
			b.Fatal(err)
		}
	}
}
