package personal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/aries1980/lodo/internal/jsonx"
)

// miniFake is a minimal notes fake: list, get, create, update.
type miniFake struct {
	mu    sync.Mutex
	notes map[string]map[string]any
	order []string
	next  int
}

func newMiniFake() *miniFake {
	return &miniFake{notes: map[string]map[string]any{}}
}

func (f *miniFake) add(title string, items ...map[string]any) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	id := "notes/" + strconv.Itoa(f.next)
	body := map[string]any{"list": map[string]any{"listItems": items}}
	if items == nil {
		body = map[string]any{"text": ""}
	}
	f.notes[id] = map[string]any{"name": id, "title": title, "body": body}
	f.order = append(f.order, id)
	return id
}

func (f *miniFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := r.URL.Path
	write := func(v any) {
		data, _ := jsonx.Marshal(v)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}
	switch {
	case r.Method == http.MethodGet && p == "/v1/notes":
		var out []any
		for _, id := range f.order {
			n := f.notes[id]
			out = append(out, map[string]any{"name": n["name"], "title": n["title"]})
		}
		write(map[string]any{"notes": out})
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/v1/notes/"):
		n, ok := f.notes[strings.TrimPrefix(p, "/v1/")]
		if !ok {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		write(n)
	case r.Method == http.MethodPost && p == "/v1/notes":
		var in map[string]any
		if err := jsonx.Unmarshal(mustRead(r), &in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.next++
		in["name"] = "notes/" + strconv.Itoa(f.next)
		f.notes[in["name"].(string)] = in
		f.order = append(f.order, in["name"].(string))
		write(in)
	case r.Method == http.MethodPatch && strings.HasPrefix(p, "/v1/notes/"):
		id := strings.TrimPrefix(p, "/v1/")
		cur, ok := f.notes[id]
		if !ok {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		var in map[string]any
		if err := jsonx.Unmarshal(mustRead(r), &in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for k, v := range in {
			if k != "name" {
				cur[k] = v
			}
		}
		write(cur)
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

func TestUnconfigured(t *testing.T) {
	c, err := NewClient("token", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("want unconfigured error")
	}
	if _, err := c.Pages(context.Background()); err == nil {
		t.Fatal("want unconfigured error")
	}
	if _, err := c.Tree(context.Background(), "x"); err == nil {
		t.Fatal("want unconfigured error")
	}
}

func TestOfficialRootRejected(t *testing.T) {
	c, err := NewClient("token", "https://keep.googleapis.com", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("want configuration error for the official root")
	}
}

func TestConfiguredPassthrough(t *testing.T) {
	fk := newMiniFake()
	fk.add("day", map[string]any{"name": "notes/1/listItems/1", "text": map[string]any{"text": "TODO Write"}, "checked": false})
	srv := httptest.NewTestServer(t, fk)
	c, err := NewClient("token", srv.URL, "", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	pages, err := c.Pages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || pages[0].Name != "day" {
		t.Fatalf("pages: %+v", pages)
	}
	tree, err := c.Tree(context.Background(), "day")
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 1 || tree[0].Marker != "TODO" {
		t.Fatalf("tree: %+v", tree)
	}
}

func TestTokenRequired(t *testing.T) {
	if _, err := NewClient("", "http://example.com", "", nil); err == nil {
		t.Fatal("want token error")
	}
}
