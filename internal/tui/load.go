package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/model"
	"github.com/aries1980/lodo/internal/norm"
	"github.com/aries1980/lodo/internal/resolve"
	"github.com/aries1980/lodo/internal/run"
)

type Data struct {
	Tasks        []model.Task
	TimeMaps     []model.TimeMap
	Issues       []resolve.Issue
	Provenance   map[string]resolve.TaskProvenance
	Blocks       map[string]*logseq.Block
	PageOf       map[string]string
	GraphOf      map[string]string
	Inputs       map[string]resolve.Input
	Sources      []run.Source
	Location     *time.Location
	TimeMapsPage string
}

func Load(ctx context.Context, cfg config.Config, sources []run.Source) (Data, error) {
	location, err := cfg.Scheduling.Location()
	if err != nil {
		return Data{}, err
	}
	timeMapsPage := cfg.Logseq.PrimaryTimeMapsPage()
	out := Data{
		Provenance:   map[string]resolve.TaskProvenance{},
		Blocks:       map[string]*logseq.Block{},
		PageOf:       map[string]string{},
		GraphOf:      map[string]string{},
		Inputs:       map[string]resolve.Input{},
		Sources:      sources,
		Location:     location,
		TimeMapsPage: timeMapsPage,
	}
	var allTasks []model.Task
	var allIssues []resolve.Issue
	var maps []model.TimeMap
	first := true
	for _, s := range sources {
		res, input, blocks, pageOf, err := loadOne(ctx, s, timeMapsPage, location)
		if err != nil {
			if s.Name != "" {
				return Data{}, fmt.Errorf("graph %s: %w", s.Name, err)
			}
			return Data{}, err
		}
		key := s.Name
		out.Inputs[key] = input
		for id, b := range blocks {
			if _, dup := out.Blocks[id]; dup {
				return Data{}, fmt.Errorf("task UUID %s appears more than once; block UUIDs must be unique", id)
			}
			out.Blocks[id] = b
			out.PageOf[id] = pageOf[id]
			out.GraphOf[id] = s.Name
		}
		allTasks = append(allTasks, res.Tasks...)
		for id, p := range res.Provenance {
			out.Provenance[id] = p
		}
		if first {
			maps = res.TimeMaps
			allIssues = append(allIssues, res.Issues...)
			first = false
			continue
		}
		for _, is := range res.Issues {
			switch is.Kind {
			case resolve.IssueMissingTimeMapsPage, resolve.IssueDefaultMapCount,
				resolve.IssueDuplicateMap, resolve.IssueBadWindow,
				resolve.IssueBadTimeMap, resolve.IssueBadDefault:
				continue
			}
			allIssues = append(allIssues, is)
		}
	}
	if err := checkDuplicateTaskIDs(allTasks); err != nil {
		return Data{}, err
	}
	out.Tasks = allTasks
	out.TimeMaps = maps
	out.Issues = allIssues
	return out, nil
}

func loadOne(ctx context.Context, s run.Source, timeMapsPage string, loc *time.Location) (resolve.Result, resolve.Input, map[string]*logseq.Block, map[string]string, error) {
	pages, err := s.Client.Pages(ctx)
	if err != nil {
		return resolve.Result{}, resolve.Input{}, nil, nil, err
	}
	sortPages(pages)
	tmName := norm.ID(timeMapsPage)
	input := resolve.Input{
		Graph:          s.Name,
		GraphOrder:     s.Order,
		DefaultTimeMap: s.DefaultTimeMap,
		Pages:          pages,
		Trees:          make(map[string][]*logseq.Block, len(pages)),
		TimeMapsPage:   timeMapsPage,
		Location:       loc,
	}
	for _, p := range pages {
		tree, err := s.Client.Tree(ctx, p.Name)
		if err != nil {
			return resolve.Result{}, resolve.Input{}, nil, nil, err
		}
		input.Trees[p.Name] = tree
		if norm.ID(p.Name) != tmName && !hasCandidate(tree) {
			continue
		}
		info, err := s.Client.Page(ctx, p.Name)
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
	blocks := map[string]*logseq.Block{}
	pageOf := map[string]string{}
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

func sortPages(pages []*logseq.Page) {
	slices.SortFunc(pages, func(a, b *logseq.Page) int {
		if c := strings.Compare(norm.ID(a.Name), norm.ID(b.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
}

func hasCandidate(tree []*logseq.Block) bool {
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

func checkDuplicateTaskIDs(tasks []model.Task) error {
	seen := make(map[string]model.Task, len(tasks))
	for _, t := range tasks {
		if prev, ok := seen[t.ID]; ok {
			if prev.Graph == t.Graph {
				return fmt.Errorf("task UUID %s appears more than once; block UUIDs must be unique", t.ID)
			}
			return fmt.Errorf("task UUID %s appears in multiple graphs; cloned graphs share block UUIDs", t.ID)
		}
		seen[t.ID] = t
	}
	return nil
}
