package workspace

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aries1980/lodo/internal/jsonx"
)

// fakeKeep is an in-memory Keep API fake: notes.list (paginated),
// labels.list, notes.get/create/update.
type fakeKeep struct {
	mu       sync.Mutex
	notes    map[string]*wireNote
	order    []string
	labels   map[string]string // id -> title
	next     int
	requests int
}

func newFakeKeep() *fakeKeep {
	return &fakeKeep{notes: map[string]*wireNote{}, labels: map[string]string{}}
}

func (f *fakeKeep) add(n wireNote) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	id := fmt.Sprintf("notes/%d", f.next)
	n.Name = id
	cp := n
	f.notes[id] = &cp
	f.order = append(f.order, id)
	return id
}

func (f *fakeKeep) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	p := r.URL.Path
	switch {
	case r.Method == http.MethodGet && p == "/v1/notes":
		size := 100
		if s := r.URL.Query().Get("pageSize"); s == "1" {
			size = 1
		}
		var out wireListOut
		for _, id := range f.order {
			out.Notes = append(out.Notes, *f.notes[id])
			if len(out.Notes) >= size {
				break
			}
		}
		writeJSON(w, out)
	case r.Method == http.MethodGet && p == "/v1/labels":
		var out wireLabelsOut
		for id, title := range f.labels {
			out.Labels = append(out.Labels, struct {
				Name  string `json:"name,omitempty"`
				Title string `json:"title,omitempty"`
			}{Name: id, Title: title})
		}
		writeJSON(w, out)
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/v1/notes/"):
		n, ok := f.notes[strings.TrimPrefix(p, "/v1/")]
		if !ok {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		writeJSON(w, n)
	case r.Method == http.MethodPost && p == "/v1/notes":
		var in wireNote
		if err := jsonx.Unmarshal(mustRead(r), &in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.next++
		in.Name = fmt.Sprintf("notes/%d", f.next)
		cp := in
		f.notes[in.Name] = &cp
		f.order = append(f.order, in.Name)
		writeJSON(w, cp)
	case r.Method == http.MethodPatch && strings.HasPrefix(p, "/v1/notes/"):
		id := strings.TrimPrefix(p, "/v1/")
		cur, ok := f.notes[id]
		if !ok {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		var in wireNote
		if err := jsonx.Unmarshal(mustRead(r), &in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if in.Title != "" {
			cur.Title = in.Title
		}
		if in.Body != nil {
			cur.Body = in.Body
		}
		writeJSON(w, cur)
	default:
		http.Error(w, `{"error":"nope"}`, http.StatusNotFound)
	}
}

func mustRead(r *http.Request) []byte {
	defer func() { _ = r.Body.Close() }()
	var b strings.Builder
	buf := make([]byte, 1<<20)
	for {
		n, err := r.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return []byte(b.String())
}

func writeJSON(w http.ResponseWriter, v any) {
	data, err := jsonx.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func listNote(title string, items ...wireItem) wireNote {
	return wireNote{Title: title, Body: &wireBody{List: &wireList{ListItems: items}}}
}

func li(id, text string, checked bool) wireItem {
	return wireItem{Name: id, Text: &wireText{Text: text}, Checked: checked}
}

func testClient(t *testing.T, fk *fakeKeep, label string) *Client {
	t.Helper()
	srv := httptest.NewTestServer(t, fk)
	c, err := NewClient(srv.Client(), srv.URL, label)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// requestCount reports how many HTTP requests reached the fake.
func (f *fakeKeep) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func TestListAndTree(t *testing.T) {
	fk := newFakeKeep()
	fk.add(listNote("2026-11-01",
		li("notes/1/listItems/1", "TODO Draft the report\nduration:: 90m", false),
		li("notes/1/listItems/2", "DONE Old work", true),
	))
	fk.add(listNote("Time Maps"))
	c := testClient(t, fk, "")

	ctx := context.Background()
	pages, err := c.Pages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 {
		t.Fatalf("want 2 pages, got %d", len(pages))
	}
	tree, err := c.Tree(ctx, "", "2026-11-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(tree))
	}
	if tree[0].Marker != "TODO" || tree[1].Marker != "DONE" {
		t.Fatalf("markers: %q %q", tree[0].Marker, tree[1].Marker)
	}
	if tree[0].PropsText["duration"] != "90m" {
		t.Fatalf("duration prop: %v", tree[0].PropsText)
	}
	if tree[0].UUID == "" || tree[0].UUID == tree[1].UUID {
		t.Fatalf("block UUIDs must be unique and non-empty: %q %q", tree[0].UUID, tree[1].UUID)
	}
}

func TestLabelFilter(t *testing.T) {
	fk := newFakeKeep()
	fk.labels["labels/1"] = "lodo"
	fk.add(wireNote{Title: "a", LabelIDs: []string{"labels/1"}, Body: &wireBody{List: &wireList{}}})
	fk.add(wireNote{Title: "b", Body: &wireBody{List: &wireList{}}})
	c := testClient(t, fk, "lodo")

	pages, err := c.Pages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || pages[0].Name != "a" {
		t.Fatalf("want only note a, got %+v", pages)
	}
}

func TestTimeMapsBootstrap(t *testing.T) {
	fk := newFakeKeep()
	c := testClient(t, fk, "")
	ctx := context.Background()

	if _, err := c.CreatePage(ctx, "Time Maps"); err != nil {
		t.Fatal(err)
	}
	deep, err := c.AppendBlock(ctx, "Time Maps", "Deep Work")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetBlockProperty(ctx, deep.UUID, "default", true); err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"Mon-Fri 09:00-11:00", "Sat 10:00-12:00 yellow"} {
		if _, err := c.InsertBlock(ctx, deep.UUID, w); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.AppendBlock(ctx, "Time Maps", "Errands"); err != nil {
		t.Fatal(err)
	}

	tree, err := c.Tree(ctx, "", "Time Maps")
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 2 {
		t.Fatalf("want 2 maps, got %d", len(tree))
	}
	if len(tree[0].Children) != 2 {
		t.Fatalf("want 2 windows on Deep Work, got %d", len(tree[0].Children))
	}
	if tree[0].PropsText["default"] != "true" {
		t.Fatalf("default prop: %v", tree[0].PropsText)
	}
}

func TestTextUpdate(t *testing.T) {
	fk := newFakeKeep()
	fk.add(wireNote{Title: "Time Maps", Body: &wireBody{Text: &wireText{Text: "Deep Work\n  Mon-Fri 09:00-11:00"}}})
	c := testClient(t, fk, "")
	ctx := context.Background()

	tree, err := c.Tree(ctx, "", "Time Maps")
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 1 || len(tree[0].Children) != 1 {
		t.Fatalf("tree: %+v", tree)
	}
	if err := c.UpdateBlock(ctx, tree[0].Children[0].UUID, "Mon-Fri 09:00-12:00"); err != nil {
		t.Fatal(err)
	}
	again, err := c.Tree(ctx, "", "Time Maps")
	if err != nil {
		t.Fatal(err)
	}
	if again[0].Children[0].Content != "Mon-Fri 09:00-12:00" {
		t.Fatalf("window: %q", again[0].Children[0].Content)
	}
}

func TestItemUpdateSyncsChecked(t *testing.T) {
	fk := newFakeKeep()
	fk.add(listNote("day", li("notes/1/listItems/1", "TODO Write\nduration:: 30m", false)))
	c := testClient(t, fk, "")
	ctx := context.Background()

	tree, err := c.Tree(ctx, "", "day")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.UpdateBlock(ctx, tree[0].UUID, "DONE Write\nduration:: 30m"); err != nil {
		t.Fatal(err)
	}
	again, err := c.Tree(ctx, "", "day")
	if err != nil {
		t.Fatal(err)
	}
	if again[0].Marker != "DONE" {
		t.Fatalf("marker: %q", again[0].Marker)
	}
}

func TestDuplicateTitles(t *testing.T) {
	fk := newFakeKeep()
	fk.add(listNote("day"))
	fk.add(listNote("Day"))
	c := testClient(t, fk, "")
	if _, err := c.Tree(context.Background(), "", "day"); err == nil {
		t.Fatal("want duplicate-title error")
	}
}

// TestPageReadsByUUIDSkipsTheNoteList pins the read cost: every page read
// with the id Pages reported is one notes.get, never a re-list of every
// note (ADR 0026 id-addressed reads). Pages itself is one list call.
func TestPageReadsByUUIDSkipTheNoteList(t *testing.T) {
	fk := newFakeKeep()
	for _, name := range []string{"day", "week", "Time Maps"} {
		fk.add(listNote(name, li("notes/1/listItems/1", "TODO Write\nduration:: 30m", false)))
	}
	c := testClient(t, fk, "")
	ctx := context.Background()

	pages, err := c.Pages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	listCalls := fk.requestCount()
	for _, p := range pages {
		if _, err := c.Tree(ctx, p.UUID, p.Name); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Page(ctx, p.UUID, p.Name); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := fk.requestCount()-listCalls, 2*len(pages); got != want {
		t.Fatalf("page reads issued %d requests, want %d (one get per read)", got, want)
	}
}

func TestPageReadByUUID(t *testing.T) {
	fk := newFakeKeep()
	id := fk.add(listNote("day", li("notes/1/listItems/1", "TODO Write", false)))
	c := testClient(t, fk, "")
	ctx := context.Background()

	page, err := c.Page(ctx, id, "day")
	if err != nil {
		t.Fatal(err)
	}
	if page == nil || page.Name != "day" || page.UUID != id {
		t.Fatalf("page: %+v", page)
	}
	tree, err := c.Tree(ctx, id, "day")
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 1 || tree[0].Marker != "TODO" {
		t.Fatalf("tree: %+v", tree)
	}
}

func TestPageReadByUUIDMissingNote(t *testing.T) {
	fk := newFakeKeep()
	fk.add(listNote("day"))
	c := testClient(t, fk, "")
	ctx := context.Background()

	page, err := c.Page(ctx, "notes/999", "day")
	if err != nil {
		t.Fatal(err)
	}
	if page != nil {
		t.Fatalf("missing note: %+v, want nil", page)
	}
	tree, err := c.Tree(ctx, "notes/999", "day")
	if err != nil {
		t.Fatal(err)
	}
	if tree != nil {
		t.Fatalf("missing note tree: %+v, want nil", tree)
	}
}

func TestPageReadByUUIDTrashedNote(t *testing.T) {
	fk := newFakeKeep()
	id := fk.add(wireNote{Trashed: true, Title: "gone", Body: &wireBody{Text: &wireText{Text: "x"}}})
	c := testClient(t, fk, "")
	ctx := context.Background()

	page, err := c.Page(ctx, id, "gone")
	if err != nil {
		t.Fatal(err)
	}
	if page != nil {
		t.Fatalf("trashed note: %+v, want nil", page)
	}
}

// BenchmarkTreeReads contrasts the two Tree addressing modes over a fake
// Keep with 100 notes: by-name re-lists every note per read (the cost
// before id-addressed reads), by-uuid fetches only the note asked for.
func BenchmarkTreeReads(b *testing.B) {
	fk := newFakeKeep()
	for i := range 100 {
		fk.add(listNote(fmt.Sprintf("day-%03d", i),
			li(fmt.Sprintf("notes/1/listItems/%d", i+1), "TODO Write\nduration:: 30m", false)))
	}
	srv := httptest.NewTestServer(b, fk)
	c, err := NewClient(srv.Client(), srv.URL, "")
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	pages, err := c.Pages(ctx)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("by-name", func(b *testing.B) {
		for b.Loop() {
			for _, p := range pages {
				if _, err := c.Tree(ctx, "", p.Name); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
	b.Run("by-uuid", func(b *testing.B) {
		for b.Loop() {
			for _, p := range pages {
				if _, err := c.Tree(ctx, p.UUID, p.Name); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}
