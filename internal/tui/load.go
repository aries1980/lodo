package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/model"
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
		res, input, blocks, pageOf, err := run.LoadGraph(ctx, s, timeMapsPage, location)
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
	if err := run.ValidateTaskIDs(allTasks); err != nil {
		return Data{}, err
	}
	out.Tasks = allTasks
	out.TimeMaps = maps
	out.Issues = run.RevalidateTimeMaps(allTasks, maps, timeMapsPage, allIssues)
	return out, nil
}
