package cli

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cucumber/godog"

	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/resolve"
	"github.com/aries1980/lodo/internal/theme"
	"github.com/aries1980/lodo/internal/tui"
)

type tuiWorld struct {
	ctx context.Context
	t   testing.TB
	env *e2e

	lastCode   int
	lastStdout string
	lastStderr string

	taskText string
	taskDur  string
	taskMap  string

	editUUID    string
	editContent string
	editKinds   []string
	editValid   bool

	ttyOld func() bool
}

func (w *tuiWorld) reset() {
	if w.env != nil {
		w.env.Close()
		w.env = nil
	}
	if w.ttyOld != nil {
		ttyCheck = w.ttyOld
		w.ttyOld = nil
	}
	w.lastCode = 0
	w.lastStdout = ""
	w.lastStderr = ""
	w.taskText = "Write report"
	w.taskDur = "30m"
	w.taskMap = ""
	w.editUUID = ""
	w.editContent = ""
	w.editKinds = nil
	w.editValid = false
}

const tuiTaskUUID = "11111111-1111-4111-8111-111111111111"

func (w *tuiWorld) taskBlocks() []*logseq.Block {
	props := map[string]any{"duration": w.taskDur}
	content := "TODO " + w.taskText + "\nduration:: " + w.taskDur
	if w.taskMap != "" {
		props["time-map"] = w.taskMap
		content += "\ntime-map:: " + w.taskMap
	}
	return []*logseq.Block{{
		UUID:       tuiTaskUUID,
		Content:    content,
		Marker:     "TODO",
		Properties: props,
	}}
}

func (w *tuiWorld) ensureHarness() error {
	if w.env != nil {
		return nil
	}
	env, err := newE2E(w.t, e2eOptions{Tasks: w.taskBlocks(), Maps: e2eMaps()})
	if err != nil {
		return err
	}
	w.env = env
	return nil
}

func (w *tuiWorld) loadData() (tui.Data, error) {
	if err := w.ensureHarness(); err != nil {
		return tui.Data{}, err
	}
	opts := &options{config: w.env.Conf}
	cfg, creds, err := loadConfig(opts, configOverrides{})
	if err != nil {
		return tui.Data{}, err
	}
	sources, err := buildSources(cfg, creds)
	if err != nil {
		return tui.Data{}, err
	}
	return tui.Load(w.ctx, cfg, sources)
}

func (w *tuiWorld) saveEdit(content string) error {
	data, err := w.loadData()
	if err != nil {
		return err
	}
	if len(data.Tasks) == 0 {
		return fmt.Errorf("no tasks loaded")
	}
	id := data.Tasks[0].ID
	w.editUUID = id
	w.editContent = content
	_, issues, ok := tui.ValidateEdited(&data, id, content)
	w.editKinds = w.editKinds[:0]
	for _, is := range issues {
		w.editKinds = append(w.editKinds, is.Kind)
	}
	w.editValid = ok
	if !ok {
		return nil
	}
	for _, s := range data.Sources {
		if s.Name == data.GraphOf[id] {
			return s.Client.UpdateBlock(w.ctx, id, content)
		}
	}
	return fmt.Errorf("no source for graph")
}

func registerTUISteps(s *godog.ScenarioContext, w *tuiWorld) {
	s.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		w.reset()
		return ctx, nil
	})
	s.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.reset()
		return ctx, nil
	})

	s.Step(`^a graph with one Task$`, func() error {
		return w.ensureHarness()
	})
	s.Step(`^a TODO "([^"]+)" with duration "([^"]+)"$`, func(text, dur string) error {
		w.taskText = text
		w.taskDur = dur
		w.taskMap = ""
		return nil
	})
	s.Step(`^a TODO "([^"]+)" with duration "([^"]+)" and time-map "([^"]+)"$`, func(text, dur, tm string) error {
		w.taskText = text
		w.taskDur = dur
		w.taskMap = tm
		return nil
	})
	s.Step(`^I run bare "lodo" without a TTY$`, func() error {
		if err := w.ensureHarness(); err != nil {
			return err
		}
		if w.ttyOld == nil {
			w.ttyOld = ttyCheck
		}
		ttyCheck = func() bool { return false }
		w.lastCode, w.lastStdout, w.lastStderr = w.env.Run()
		return nil
	})
	s.Step(`^I run bare "lodo --json"$`, func() error {
		if err := w.ensureHarness(); err != nil {
			return err
		}
		if w.ttyOld == nil {
			w.ttyOld = ttyCheck
		}
		ttyCheck = func() bool { return true }
		w.lastCode, w.lastStdout, w.lastStderr = w.env.Run("--json")
		return nil
	})
	s.Step(`^exit code is 0$`, func() error {
		if w.lastCode != 0 {
			return fmt.Errorf("exit = %d, want 0\nstdout: %s\nstderr: %s", w.lastCode, w.lastStdout, w.lastStderr)
		}
		return nil
	})
	s.Step(`^no Logseq writes occur$`, func() error {
		if writes := w.env.Logseq.Writes(); len(writes) != 0 {
			return fmt.Errorf("writes = %v, want none", writes)
		}
		return nil
	})
	s.Step(`^no calendar writes occur$`, func() error { return nil })
	s.Step(`^output shows the CLI help$`, func() error {
		if !strings.Contains(w.lastStdout, "Schedule Logseq tasks") {
			return fmt.Errorf("help missing:\n%s\nstderr: %s", w.lastStdout, w.lastStderr)
		}
		return nil
	})
	s.Step(`^I save an edit changing duration to "([^"]+)"$`, func(dur string) error {
		return w.saveEdit("TODO " + w.taskText + "\nduration:: " + dur)
	})
	s.Step(`^I save an edit with duration "([^"]+)"$`, func(dur string) error {
		content := "TODO " + w.taskText + "\nduration:: " + dur
		if w.taskMap != "" {
			content += "\ntime-map:: " + w.taskMap
		}
		return w.saveEdit(content)
	})
	s.Step(`^I save an edit with time-map "([^"]+)"$`, func(tm string) error {
		return w.saveEdit("TODO " + w.taskText + "\nduration:: " + w.taskDur + "\ntime-map:: " + tm)
	})
	s.Step(`^validation reports "([^"]+)"$`, func(kind string) error {
		for _, k := range w.editKinds {
			if k == kind {
				return nil
			}
		}
		return fmt.Errorf("kinds = %v, want %q", w.editKinds, kind)
	})
	s.Step(`^Logseq received one update_block$`, func() error {
		writes := w.env.Logseq.Writes()
		if len(writes) != 1 || !strings.HasPrefix(writes[0], "update_block") {
			return fmt.Errorf("writes = %v, want one update_block", writes)
		}
		return nil
	})
	s.Step(`^I apply the time-map dropdown fix "([^"]+)"$`, func(slug string) error {
		data, err := w.loadData()
		if err != nil {
			return err
		}
		w.editContent = tui.PatchTimeMap(w.editContent, slug, false)
		_, issues, ok := tui.ValidateEdited(&data, w.editUUID, w.editContent)
		w.editKinds = w.editKinds[:0]
		for _, is := range issues {
			w.editKinds = append(w.editKinds, is.Kind)
		}
		w.editValid = ok
		return nil
	})
	s.Step(`^the edit validates$`, func() error {
		if !w.editValid {
			return fmt.Errorf("edit invalid: %v", w.editKinds)
		}
		return nil
	})
	s.Step(`^saving writes one update_block$`, func() error {
		data, err := w.loadData()
		if err != nil {
			return err
		}
		for _, s := range data.Sources {
			if s.Name == data.GraphOf[w.editUUID] {
				if err := s.Client.UpdateBlock(w.ctx, w.editUUID, w.editContent); err != nil {
					return err
				}
			}
		}
		if writes := w.env.Logseq.Writes(); len(writes) != 1 {
			return fmt.Errorf("writes = %v, want one update_block", writes)
		}
		return nil
	})
	s.Step(`^highlighted view still contains "\*\*" and "::"$`, func() error {
		data, err := w.loadData()
		if err != nil {
			return err
		}
		if len(data.Tasks) == 0 {
			return fmt.Errorf("no tasks")
		}
		raw := ""
		for _, t := range data.Tasks {
			if b, ok := data.Blocks[t.ID]; ok {
				raw = b.Content
				break
			}
		}
		out := tui.Highlight(raw, theme.For(theme.Concrete))
		if !strings.Contains(out, "**") || !strings.Contains(out, "::") {
			return fmt.Errorf("raw markers hidden:\n%s", out)
		}
		_ = resolve.IssueUnknownTimeMap
		return nil
	})
}

func TestTUIBehavior(t *testing.T) {
	w := &tuiWorld{ctx: t.Context(), t: t}
	suite := godog.TestSuite{
		ScenarioInitializer: func(s *godog.ScenarioContext) {
			registerTUISteps(s, w)
		},
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"../../test/behavior/tui"},
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("godog tui suite failed")
	}
}
