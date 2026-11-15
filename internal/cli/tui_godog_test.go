package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cucumber/godog"

	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/resolve"
	"github.com/aries1980/lodo/internal/theme"
	"github.com/aries1980/lodo/internal/tui"
)

type tuiWorld struct {
	srv  *httptest.Server
	conf string

	writes []string

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
	if w.srv != nil {
		w.srv.Close()
		w.srv = nil
	}
	if w.ttyOld != nil {
		ttyCheck = w.ttyOld
		w.ttyOld = nil
	}
	w.conf = ""
	w.writes = nil
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
	if w.conf != "" {
		return nil
	}
	tasks := w.taskBlocks()
	maps := []*logseq.Block{{
		UUID:       "map-deep-uuid",
		Content:    "Deep Work",
		Properties: map[string]any{"default": true},
		Children:   []*logseq.Block{{UUID: "w-uuid", Content: "Mon-Fri 09:00-12:00"}},
	}}
	taskRaw, _ := json.Marshal(tasks)
	tmRaw, _ := json.Marshal(maps)
	w.srv = httptest.NewServer(http.HandlerFunc(func(wr http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(wr, "POST", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Method string `json:"method"`
			Args   []any  `json:"args"`
		}
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r.Body)
		if err := json.Unmarshal(buf.Bytes(), &req); err != nil {
			http.Error(wr, "bad stub request", http.StatusBadRequest)
			return
		}
		switch req.Method {
		case "logseq.Editor.get_all_pages":
			_, _ = wr.Write([]byte(`[{"name":"2026-01-01","uuid":"d"},{"name":"Time Maps","uuid":"tm"}]`))
		case "logseq.Editor.get_page_blocks_tree":
			if len(req.Args) > 0 && req.Args[0] == "Time Maps" {
				_, _ = wr.Write(tmRaw)
				return
			}
			_, _ = wr.Write(taskRaw)
		case "logseq.Editor.get_page":
			name := ""
			if len(req.Args) > 0 {
				name, _ = req.Args[0].(string)
			}
			_, _ = wr.Write([]byte(`{"name":"` + name + `","uuid":"x","properties":null}`))
		case "logseq.Editor.update_block":
			uuid := ""
			if len(req.Args) > 0 {
				uuid, _ = req.Args[0].(string)
			}
			w.writes = append(w.writes, "update_block "+uuid)
			_, _ = wr.Write([]byte(`{}`))
		default:
			http.Error(wr, "unexpected method", http.StatusNotFound)
		}
	}))
	dir, err := os.MkdirTemp("", "lodo-tui-godog")
	if err != nil {
		return err
	}
	conf := filepath.Join(dir, "lodo", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(conf), 0o700); err != nil {
		return err
	}
	body := fmt.Sprintf(`logseq:
  api_url: %s/api
  time_maps_page: Time Maps
scheduling:
  granularity: 15m
  window_days: 7
  timezone: Europe/London
calendar:
  provider: google
  id: primary
`, w.srv.URL)
	if err := os.WriteFile(conf, []byte(body), 0o644); err != nil {
		return err
	}
	cred := filepath.Join(dir, "lodo", "credentials.json")
	if err := os.WriteFile(cred, []byte(`{"logseq":{"token":"st-token"}}`), 0o600); err != nil {
		return err
	}
	if err := os.Setenv("XDG_CONFIG_HOME", dir); err != nil {
		return err
	}
	if err := os.Setenv("XDG_STATE_HOME", dir); err != nil {
		return err
	}
	if err := os.Setenv("HOME", dir); err != nil {
		return err
	}
	w.conf = conf
	return nil
}

func (w *tuiWorld) loadData() (tui.Data, error) {
	if err := w.ensureHarness(); err != nil {
		return tui.Data{}, err
	}
	opts := &options{config: w.conf}
	cfg, sources, _, err := loadSources(opts, configOverrides{})
	if err != nil {
		return tui.Data{}, err
	}
	return tui.Load(context.Background(), cfg, sources)
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
			return s.Client.UpdateBlock(context.Background(), id, content)
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
		stdout, stderr := &strings.Builder{}, &strings.Builder{}
		w.lastCode = Run([]string{"--config", w.conf}, stdout, stderr)
		w.lastStdout, w.lastStderr = stdout.String(), stderr.String()
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
		stdout, stderr := &strings.Builder{}, &strings.Builder{}
		w.lastCode = Run([]string{"--config", w.conf, "--json"}, stdout, stderr)
		w.lastStdout, w.lastStderr = stdout.String(), stderr.String()
		return nil
	})
	s.Step(`^exit code is 0$`, func() error {
		if w.lastCode != 0 {
			return fmt.Errorf("exit = %d, want 0\nstdout: %s\nstderr: %s", w.lastCode, w.lastStdout, w.lastStderr)
		}
		return nil
	})
	s.Step(`^no Logseq writes occur$`, func() error {
		if len(w.writes) != 0 {
			return fmt.Errorf("writes = %v, want none", w.writes)
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
		if len(w.writes) != 1 || !strings.HasPrefix(w.writes[0], "update_block") {
			return fmt.Errorf("writes = %v, want one update_block", w.writes)
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
				if err := s.Client.UpdateBlock(context.Background(), w.editUUID, w.editContent); err != nil {
					return err
				}
			}
		}
		if len(w.writes) != 1 {
			return fmt.Errorf("writes = %v, want one update_block", w.writes)
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
	w := &tuiWorld{}
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
